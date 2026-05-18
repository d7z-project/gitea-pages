package filters

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gopkg.d7z.net/gitea-pages/pkg/core"
)

type (
	proxyLookupNetIPFunc func(context.Context, string, string) ([]netip.Addr, error)
	proxyDialContextFunc func(context.Context, string, string) (net.Conn, error)
)

type proxyGlobalConfig struct {
	ForwardAuthorization bool     `json:"forward_authorization"`
	DenyHosts            []string `json:"deny_hosts"`
	DenyCIDRs            []string `json:"deny_cidrs"`
	StripRequestHeaders  any      `json:"strip_request_headers"`
}

type proxyRouteConfig struct {
	Prefix string `json:"prefix"`
	Target string `json:"target"`
}

type proxyPolicy struct {
	forwardAuthorization bool
	denyHosts            map[string]struct{}
	denyCIDRs            []netip.Prefix
	resolver             proxyLookupNetIPFunc
	dialContext          proxyDialContextFunc
	transportTemplate    *http.Transport
}

func FilterInstProxy(init core.GlobalFilterInit) (core.FilterInstance, error) {
	var global proxyGlobalConfig
	if init.Config != nil {
		if err := init.Config.Unmarshal(&global); err != nil {
			return nil, err
		}
	}
	policy, err := newProxyPolicy(global)
	if err != nil {
		return nil, err
	}

	return func(config core.Params) (core.FilterCall, error) {
		var param proxyRouteConfig
		if err := config.Unmarshal(&param); err != nil {
			return nil, err
		}
		if strings.TrimSpace(param.Target) == "" {
			return nil, errors.New("reverse_proxy target is required")
		}
		targetURL, err := parseProxyTarget(param.Target)
		if err != nil {
			return nil, err
		}

		return func(ctx core.FilterContext, writer http.ResponseWriter, request *http.Request, next core.NextCall) error {
			proxyPath := "/" + ctx.Path
			targetPath := strings.TrimPrefix(proxyPath, param.Prefix)
			if !strings.HasPrefix(targetPath, "/") {
				targetPath = "/" + targetPath
			}

			transport, err := policy.transportForTarget(request.Context(), targetURL)
			if err != nil {
				return err
			}
			defer transport.CloseIdleConnections()

			proxy := &httputil.ReverseProxy{
				Transport: transport,
				Rewrite: func(pr *httputil.ProxyRequest) {
					rewriteProxyRequest(pr, request, targetURL, targetPath, policy.forwardAuthorization, ctx)
				},
			}
			slog.Debug("proxy route matched", "prefix", param.Prefix, "target", param.Target,
				"path", proxyPath, "resolved_target", fmt.Sprintf("%s%s", targetURL, targetPath))
			proxy.ServeHTTP(writer, request)
			return nil
		}, nil
	}, nil
}

func newProxyPolicy(global proxyGlobalConfig) (*proxyPolicy, error) {
	if global.StripRequestHeaders != nil {
		return nil, errors.New("reverse_proxy.strip_request_headers is no longer supported; use forward_authorization instead")
	}

	policy := &proxyPolicy{
		forwardAuthorization: global.ForwardAuthorization,
		denyHosts:            make(map[string]struct{}, len(global.DenyHosts)),
		denyCIDRs:            make([]netip.Prefix, 0, len(global.DenyCIDRs)),
		resolver:             net.DefaultResolver.LookupNetIP,
		dialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		transportTemplate: http.DefaultTransport.(*http.Transport).Clone(),
	}
	policy.transportTemplate.Proxy = nil
	policy.transportTemplate.DisableKeepAlives = true

	for _, raw := range global.DenyHosts {
		host := normalizeProxyHost(raw)
		switch {
		case host == "":
			return nil, errors.New("reverse_proxy deny_hosts contains an empty host")
		case strings.ContainsAny(host, " \t\r\n") || strings.Contains(host, "://") || strings.ContainsAny(host, "/?#@"):
			return nil, fmt.Errorf("reverse_proxy deny_hosts entry is invalid: %s", raw)
		case strings.Contains(host, ":"):
			return nil, fmt.Errorf("reverse_proxy deny_hosts entry must not include a port or IP literal: %s", raw)
		}
		if _, err := netip.ParseAddr(host); err == nil {
			return nil, fmt.Errorf("reverse_proxy deny_hosts entry must be a hostname: %s", raw)
		}
		policy.denyHosts[host] = struct{}{}
	}

	for _, raw := range global.DenyCIDRs {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			return nil, errors.New("reverse_proxy deny_cidrs contains an empty entry")
		}

		var (
			prefix netip.Prefix
			err    error
		)
		if strings.Contains(entry, "/") {
			prefix, err = netip.ParsePrefix(entry)
			if err != nil {
				return nil, fmt.Errorf("reverse_proxy deny_cidrs entry is invalid: %s", raw)
			}
			prefix = prefix.Masked()
			if addr := prefix.Addr(); addr.Is4In6() && prefix.Bits() >= 96 {
				prefix = netip.PrefixFrom(addr.Unmap(), prefix.Bits()-96).Masked()
			}
		} else {
			addr, parseErr := netip.ParseAddr(entry)
			if parseErr != nil {
				return nil, fmt.Errorf("reverse_proxy deny_cidrs entry is invalid: %s", raw)
			}
			addr = addr.Unmap()
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		policy.denyCIDRs = append(policy.denyCIDRs, prefix)
	}

	return policy, nil
}

func (p *proxyPolicy) transportForTarget(ctx context.Context, target *url.URL) (*http.Transport, error) {
	host := normalizeProxyHost(target.Hostname())
	if host == "" {
		return nil, errors.New("reverse_proxy target missing hostname")
	}
	if _, denied := p.denyHosts[host]; denied {
		return nil, fmt.Errorf("reverse_proxy target host %s is denied", host)
	}

	port := target.Port()
	if port == "" {
		port = "443"
	}

	candidates := make([]string, 0, 4)
	if addr, err := netip.ParseAddr(host); err == nil {
		addr = addr.Unmap()
		if p.isDeniedAddr(addr) {
			return nil, fmt.Errorf("reverse_proxy target %s is denied by cidr rule", addr)
		}
		candidates = append(candidates, net.JoinHostPort(addr.String(), port))
	} else {
		ips, err := p.resolver(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			ip = ip.Unmap()
			if !ip.IsValid() || p.isDeniedAddr(ip) {
				continue
			}
			candidates = append(candidates, net.JoinHostPort(ip.String(), port))
		}
		if len(candidates) == 0 {
			return nil, fmt.Errorf("reverse_proxy target %s resolved only to denied addresses", host)
		}
	}

	transport := p.transportTemplate
	if transport == nil {
		transport = http.DefaultTransport.(*http.Transport).Clone()
	}
	transport = transport.Clone()
	transport.Proxy = nil
	transport.DisableKeepAlives = true
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		dial := p.dialContext
		if dial == nil {
			dial = (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext
		}

		dialErrs := make([]error, 0, len(candidates))
		for _, candidate := range candidates {
			conn, err := dial(ctx, network, candidate)
			if err == nil {
				return conn, nil
			}
			dialErrs = append(dialErrs, fmt.Errorf("%s: %w", candidate, err))
		}
		if len(dialErrs) == 0 {
			return nil, fmt.Errorf("reverse_proxy target %s has no allowed dial candidates", host)
		}
		return nil, errors.Join(dialErrs...)
	}

	return transport, nil
}

func (p *proxyPolicy) isDeniedAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, prefix := range p.denyCIDRs {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func rewriteProxyRequest(pr *httputil.ProxyRequest, in *http.Request, target *url.URL, targetPath string, forwardAuthorization bool, ctx core.FilterContext) {
	pr.Out.URL.Path = targetPath
	pr.Out.URL.RawPath = targetPath
	pr.SetURL(target)
	if target.RawQuery == "" || pr.Out.URL.RawQuery == "" {
		pr.Out.URL.RawQuery = target.RawQuery + pr.Out.URL.RawQuery
	} else {
		pr.Out.URL.RawQuery = target.RawQuery + "&" + pr.Out.URL.RawQuery
	}
	sanitizeProxyRequestHeaders(pr.Out.Header, forwardAuthorization)
	origin := core.RequestInfoFromRequest(in)
	pr.Out.Header.Set("X-Real-IP", origin.ClientIP)
	pr.Out.Header.Set("X-Page-IP", origin.ClientIP)
	pr.Out.Header.Set("X-Page-Refer", fmt.Sprintf("%s/%s/%s", ctx.Owner, ctx.Repo, ctx.Path))
	pr.Out.Header.Set("X-Page-Host", in.Host)
	pr.Out.Header.Set("X-Forwarded-Host", in.Host)
	pr.Out.Header.Set("X-Forwarded-Proto", origin.Scheme)
	setForwardingHeaders(pr.Out.Header, origin, in.Host)
}

func parseProxyTarget(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	target, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if target.Scheme == "" || target.Host == "" || target.Hostname() == "" {
		return nil, errors.New("reverse_proxy target must be an absolute URL")
	}
	if !strings.EqualFold(target.Scheme, "https") {
		return nil, fmt.Errorf("reverse_proxy target must use https: %s", raw)
	}
	if target.User != nil {
		return nil, errors.New("reverse_proxy target must not include userinfo")
	}
	if port := target.Port(); port != "" {
		if _, err := strconv.Atoi(port); err != nil {
			return nil, fmt.Errorf("reverse_proxy target has invalid port: %s", raw)
		}
	} else if strings.Contains(target.Host, ":") && !(strings.HasPrefix(target.Host, "[") && strings.HasSuffix(target.Host, "]")) {
		return nil, fmt.Errorf("reverse_proxy target has invalid port: %s", raw)
	}
	return target, nil
}

func sanitizeProxyRequestHeaders(headers http.Header, forwardAuthorization bool) {
	headers.Del("Forwarded")
	headers.Del("Proxy-Authorization")
	headers.Del("X-Forwarded-For")
	headers.Del("X-Forwarded-Host")
	headers.Del("X-Forwarded-Proto")
	headers.Del("X-Page-Host")
	headers.Del("X-Page-IP")
	headers.Del("X-Page-Refer")
	headers.Del("X-Real-IP")
	if !forwardAuthorization {
		headers.Del("Authorization")
	}
}

func setForwardingHeaders(headers http.Header, origin core.RequestInfo, host string) {
	if origin.ClientIP == "" {
		return
	}
	headers.Set("X-Forwarded-For", origin.ClientIP)
	headers.Set("Forwarded", formatForwardedHeader(origin.ClientIP, origin.Scheme, host))
}

func formatForwardedHeader(clientIP, scheme, host string) string {
	element := "for=" + formatForwardedNode(clientIP)
	if scheme == "http" || scheme == "https" {
		element += ";proto=" + scheme
	}
	if host != "" {
		element += ";host=" + formatForwardedValue(host)
	}
	return element
}

func formatForwardedNode(addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.Contains(addr, ":") && !strings.HasPrefix(addr, "[") {
		return formatForwardedValue("[" + addr + "]")
	}
	return formatForwardedValue(addr)
}

func formatForwardedValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, `:;, "=\\`) {
		replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
		return `"` + replacer.Replace(value) + `"`
	}
	return value
}

func normalizeProxyHost(raw string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
}
