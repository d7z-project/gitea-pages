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
	"strings"
	"time"

	"gopkg.d7z.net/gitea-pages/pkg/core"
)

var defaultProxyStripHeaders = []string{
	"Authorization",
	"Cookie",
	"Forwarded",
	"Proxy-Authorization",
	"X-Forwarded-For",
	"X-Forwarded-Host",
	"X-Forwarded-Proto",
	"X-Page-Host",
	"X-Page-IP",
	"X-Page-Refer",
	"X-Real-IP",
}

type proxyGlobalConfig struct {
	StripRequestHeaders []string `json:"strip_request_headers"`
}

type proxyRouteConfig struct {
	Prefix string `json:"prefix"`
	Target string `json:"target"`
}

func FilterInstProxy(globalParams core.Params) (core.FilterInstance, error) {
	var global proxyGlobalConfig
	if globalParams != nil {
		if err := globalParams.Unmarshal(&global); err != nil {
			return nil, err
		}
	}
	if len(global.StripRequestHeaders) == 0 {
		global.StripRequestHeaders = append([]string(nil), defaultProxyStripHeaders...)
	} else {
		global.StripRequestHeaders = normalizeHeaderNames(global.StripRequestHeaders)
	}
	transport := newProxyTransport()
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
		stripHeaders := append([]string(nil), global.StripRequestHeaders...)

		return func(ctx core.FilterContext, writer http.ResponseWriter, request *http.Request, next core.NextCall) error {
			proxyPath := "/" + ctx.Path
			targetPath := strings.TrimPrefix(proxyPath, param.Prefix)
			if !strings.HasPrefix(targetPath, "/") {
				targetPath = "/" + targetPath
			}
			proxy := &httputil.ReverseProxy{
				Transport: transport,
				Rewrite: func(pr *httputil.ProxyRequest) {
					rewriteProxyRequest(pr, request, targetURL, targetPath, stripHeaders, ctx)
				},
			}
			slog.Debug("proxy route matched", "prefix", param.Prefix, "target", param.Target,
				"path", proxyPath, "resolved_target", fmt.Sprintf("%s%s", targetURL, targetPath))
			proxy.ServeHTTP(writer, request)
			return nil
		}, nil
	}, nil
}

func rewriteProxyRequest(pr *httputil.ProxyRequest, in *http.Request, target *url.URL, targetPath string, stripHeaders []string, ctx core.FilterContext) {
	pr.Out.URL.Path = targetPath
	pr.Out.URL.RawPath = targetPath
	pr.SetURL(target)
	if target.RawQuery == "" || pr.Out.URL.RawQuery == "" {
		pr.Out.URL.RawQuery = target.RawQuery + pr.Out.URL.RawQuery
	} else {
		pr.Out.URL.RawQuery = target.RawQuery + "&" + pr.Out.URL.RawQuery
	}
	for _, header := range stripHeaders {
		pr.Out.Header.Del(header)
	}
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
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, errors.New("reverse_proxy target must be an absolute URL")
	}
	switch strings.ToLower(target.Scheme) {
	case "https":
	default:
		return nil, fmt.Errorf("reverse_proxy target must use https: %s", raw)
	}
	return target, nil
}

func normalizeHeaderNames(headers []string) []string {
	seen := make(map[string]struct{}, len(headers))
	result := make([]string, 0, len(headers))
	for _, header := range headers {
		header = http.CanonicalHeaderKey(strings.TrimSpace(header))
		if header == "" {
			continue
		}
		if _, ok := seen[header]; ok {
			continue
		}
		seen[header] = struct{}{}
		result = append(result, header)
	}
	return result
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

func newProxyTransport() http.RoundTripper {
	base := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		if direct, err := netip.ParseAddr(host); err == nil {
			if isPrivateTargetAddr(direct) {
				return nil, fmt.Errorf("reverse_proxy target %s resolves to private address", direct)
			}
			return dialer.DialContext(ctx, network, addr)
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if isPrivateTargetAddr(ip) {
				return nil, fmt.Errorf("reverse_proxy target %s resolves to private address", ip)
			}
		}
		return dialer.DialContext(ctx, network, addr)
	}
	return base
}

func isPrivateTargetAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() {
		return true
	}
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalMulticast() || addr.IsLinkLocalUnicast()
}
