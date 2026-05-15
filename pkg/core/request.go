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
	ClientIP string
	PeerIP   string
	Scheme   string
	Host     string
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
			policy.prefixes = append(policy.prefixes, normalizeTrustedPrefix(prefix))
			continue
		}
		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, err
		}
		policy.prefixes = append(policy.prefixes, normalizeTrustedPrefix(netip.PrefixFrom(addr, addr.BitLen())))
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
	peerIP, peerAddr, peerAddrOK := resolvePeerAddr(r)
	info := RequestInfo{
		ClientIP: peerIP,
		PeerIP:   peerIP,
		Scheme:   "http",
		Host:     "",
	}
	if r != nil {
		info.Host = r.Host
	}
	if r != nil && r.TLS != nil {
		info.Scheme = "https"
	}
	if r == nil {
		return info
	}
	if policy == nil || !peerAddrOK || !policy.isTrusted(peerAddr) {
		return info
	}
	clientIP, scheme, host := parseTrustedForwarding(r, policy, peerAddr)
	if clientIP != "" {
		info.ClientIP = clientIP
	}
	if scheme != "" {
		info.Scheme = scheme
	}
	if host != "" {
		info.Host = host
	}
	return info
}

func resolvePeerAddr(r *http.Request) (string, netip.Addr, bool) {
	if r == nil {
		return "", netip.Addr{}, false
	}
	peerIP := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(peerIP); err == nil {
		peerIP = host
	}
	peerAddr, ok := parseAddr(peerIP)
	if ok {
		peerIP = peerAddr.String()
	}
	return peerIP, peerAddr, ok
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

func normalizeTrustedPrefix(prefix netip.Prefix) netip.Prefix {
	prefix = prefix.Masked()
	addr := prefix.Addr()
	if addr.Is4In6() && prefix.Bits() >= 96 {
		return netip.PrefixFrom(addr.Unmap(), prefix.Bits()-96).Masked()
	}
	return prefix
}

func parseTrustedForwarding(r *http.Request, policy *TrustedProxyPolicy, peerAddr netip.Addr) (string, string, string) {
	if clientIP, scheme, host, ok := parseForwardedValues(r.Header.Values("Forwarded"), policy, peerAddr); ok {
		return clientIP, scheme, host
	}
	return parseLegacyForwardedValues(
		r.Header.Values("X-Forwarded-For"),
		r.Header.Values("X-Forwarded-Proto"),
		r.Header.Values("X-Forwarded-Host"),
		r.Header.Values("X-Real-IP"),
		policy,
		peerAddr,
	)
}

func parseLegacyForwardedValues(forValues, protoValues, hostValues, realIPValues []string, policy *TrustedProxyPolicy, peerAddr netip.Addr) (string, string, string) {
	chain := make([]netip.Addr, 0, 8)
	for _, value := range forValues {
		for _, part := range strings.Split(value, ",") {
			if addr, ok := parseAddr(part); ok {
				chain = append(chain, addr)
			}
		}
	}
	clientIP := resolveClientIPFromChain(chain, policy, peerAddr)
	if clientIP == "" {
		for _, value := range realIPValues {
			if addr, ok := parseAddr(value); ok {
				clientIP = addr.String()
				break
			}
		}
	}
	return clientIP, firstForwardedProto(protoValues), firstForwardedHost(hostValues)
}

func parseForwardedValues(values []string, policy *TrustedProxyPolicy, peerAddr netip.Addr) (string, string, string, bool) {
	chain := make([]netip.Addr, 0, 8)
	proto := ""
	host := ""
	for _, value := range values {
		for _, element := range splitHeaderValues(value, ',') {
			var forwardedAddr netip.Addr
			var hasForwardedAddr bool
			for _, param := range splitHeaderValues(element, ';') {
				key, rawValue, ok := strings.Cut(param, "=")
				if !ok {
					continue
				}
				key = strings.ToLower(strings.TrimSpace(key))
				rawValue = trimQuotedString(strings.TrimSpace(rawValue))
				switch key {
				case "for":
					if hasForwardedAddr {
						continue
					}
					if addr, ok := parseForwardedNode(rawValue); ok {
						forwardedAddr = addr
						hasForwardedAddr = true
					}
				case "proto":
					if proto != "" {
						continue
					}
					current := strings.ToLower(strings.TrimSpace(rawValue))
					if current == "http" || current == "https" {
						proto = current
					}
				case "host":
					if host == "" {
						host = rawValue
					}
				}
			}
			if hasForwardedAddr {
				chain = append(chain, forwardedAddr)
			}
		}
	}
	if len(chain) == 0 {
		return "", "", "", false
	}
	return resolveClientIPFromChain(chain, policy, peerAddr), proto, host, true
}

func resolveClientIPFromChain(chain []netip.Addr, policy *TrustedProxyPolicy, peerAddr netip.Addr) string {
	if len(chain) > 0 {
		for i := len(chain) - 1; i >= 0; i-- {
			if !policy.isTrusted(chain[i]) {
				return chain[i].String()
			}
		}
		return chain[0].String()
	}
	if !policy.isTrusted(peerAddr) {
		return peerAddr.String()
	}
	return ""
}

func firstForwardedProto(values []string) string {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.ToLower(strings.TrimSpace(part))
			if part == "http" || part == "https" {
				return part
			}
		}
	}
	return ""
}

func firstForwardedHost(values []string) string {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				return part
			}
		}
	}
	return ""
}

func parseForwardedNode(raw string) (netip.Addr, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Addr{}, false
	}
	if strings.EqualFold(raw, "unknown") || strings.HasPrefix(raw, "_") {
		return netip.Addr{}, false
	}
	return parseAddr(raw)
}

func splitHeaderValues(raw string, sep rune) []string {
	parts := make([]string, 0, 4)
	var current strings.Builder
	inQuotes := false
	escape := false
	for _, r := range raw {
		switch {
		case escape:
			current.WriteRune(r)
			escape = false
		case r == '\\' && inQuotes:
			escape = true
		case r == '"':
			inQuotes = !inQuotes
			current.WriteRune(r)
		case r == sep && !inQuotes:
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	parts = append(parts, strings.TrimSpace(current.String()))
	return parts
}

func trimQuotedString(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		raw = raw[1 : len(raw)-1]
	}
	raw = strings.ReplaceAll(raw, `\"`, `"`)
	raw = strings.ReplaceAll(raw, `\\`, `\`)
	return raw
}
