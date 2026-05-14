package core

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type requestInfoContextKey struct{}

type TrustedProxyPolicy struct {
	prefixes []netip.Prefix
}

type RequestInfo struct {
	ClientIP     string
	PeerIP       string
	Scheme       string
	TrustedProxy bool
}

func NewTrustedProxyPolicy(entries []string) (*TrustedProxyPolicy, error) {
	policy := &TrustedProxyPolicy{prefixes: make([]netip.Prefix, 0, len(entries))}
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			prefix, err := netip.ParsePrefix(entry)
			if err != nil {
				return nil, err
			}
			policy.prefixes = append(policy.prefixes, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, err
		}
		policy.prefixes = append(policy.prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return policy, nil
}

func (p *TrustedProxyPolicy) isTrusted(addr netip.Addr) bool {
	if p == nil {
		return false
	}
	addr = addr.Unmap()
	for _, prefix := range p.prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func ContextWithRequestInfo(ctx context.Context, info RequestInfo) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, requestInfoContextKey{}, info)
}

func RequestInfoFromRequest(r *http.Request) RequestInfo {
	if r == nil {
		return RequestInfo{Scheme: "http"}
	}
	if info, ok := r.Context().Value(requestInfoContextKey{}).(RequestInfo); ok {
		return info
	}
	return ResolveRequestInfo(r, nil)
}

func ResolveRequestInfo(r *http.Request, policy *TrustedProxyPolicy) RequestInfo {
	peerIP := ""
	if r != nil {
		peerIP = strings.TrimSpace(r.RemoteAddr)
		if host, _, err := net.SplitHostPort(peerIP); err == nil {
			peerIP = host
		}
	}
	info := RequestInfo{
		ClientIP: peerIP,
		PeerIP:   peerIP,
		Scheme:   "http",
	}
	if r != nil && r.TLS != nil {
		info.Scheme = "https"
	}
	if r == nil {
		return info
	}
	peerAddr, ok := parseAddr(peerIP)
	if policy == nil || !ok || !policy.isTrusted(peerAddr) {
		return info
	}
	info.TrustedProxy = true

	chain := make([]netip.Addr, 0, 8)
	for _, value := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(value, ",") {
			if addr, ok := parseAddr(part); ok {
				chain = append(chain, addr)
			}
		}
	}
	chain = append(chain, peerAddr)
	for i := len(chain) - 1; i >= 0; i-- {
		if !policy.isTrusted(chain[i]) {
			info.ClientIP = chain[i].String()
			break
		}
	}
	if info.ClientIP == peerIP {
		if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
			if addr, ok := parseAddr(realIP); ok {
				info.ClientIP = addr.String()
			}
		} else if len(chain) > 0 {
			info.ClientIP = chain[0].String()
		}
	}
	for _, value := range r.Header.Values("X-Forwarded-Proto") {
		for _, part := range strings.Split(value, ",") {
			part = strings.ToLower(strings.TrimSpace(part))
			if part == "http" || part == "https" {
				info.Scheme = part
				return info
			}
		}
	}
	return info
}

func parseAddr(raw string) (netip.Addr, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Addr{}, false
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	addr, err := netip.ParseAddr(strings.Trim(raw, "[]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}
