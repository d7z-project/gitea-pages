package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRequestInfoIgnoresUntrustedForwardedHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
	req.RemoteAddr = "198.51.100.20:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	req.Header.Set("X-Forwarded-Proto", "https")

	info := ResolveRequestInfo(req, nil)
	assert.Equal(t, "198.51.100.20", info.ClientIP)
	assert.Equal(t, "198.51.100.20", info.PeerIP)
	assert.Equal(t, "http", info.Scheme)
	assert.Equal(t, "example.com", info.Host)
}

func TestResolveRequestInfoTrustsConfiguredProxyChain(t *testing.T) {
	policy, err := NewTrustedProxyPolicy([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
	req.RemoteAddr = "10.0.0.2:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.10, 10.0.0.1")
	req.Header.Set("X-Forwarded-Proto", "https")

	info := ResolveRequestInfo(req, policy)
	assert.Equal(t, "198.51.100.10", info.ClientIP)
	assert.Equal(t, "10.0.0.2", info.PeerIP)
	assert.Equal(t, "https", info.Scheme)
	assert.Equal(t, "example.com", info.Host)
}

func TestResolveRequestInfoTrustsConfiguredIPv6ProxyChain(t *testing.T) {
	policy, err := NewTrustedProxyPolicy([]string{"2001:db8:1::/48"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
	req.RemoteAddr = "[2001:db8:1::2]:1234"
	req.Header.Set("X-Forwarded-For", "[2001:db8:feed::10]:4321, 2001:db8:1::1")
	req.Header.Set("X-Forwarded-Proto", "https")

	info := ResolveRequestInfo(req, policy)
	assert.Equal(t, "2001:db8:feed::10", info.ClientIP)
	assert.Equal(t, "2001:db8:1::2", info.PeerIP)
	assert.Equal(t, "https", info.Scheme)
	assert.Equal(t, "example.com", info.Host)
}

func TestResolveRequestInfoUsesForwardedHeader(t *testing.T) {
	policy, err := NewTrustedProxyPolicy([]string{"2001:db8:1::/48"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
	req.RemoteAddr = "[2001:db8:1::2]:1234"
	req.Header.Set("Forwarded", `for="[2001:db8:feed::10]";proto=https;host="pages.example", for="[2001:db8:1::1]"`)

	info := ResolveRequestInfo(req, policy)
	assert.Equal(t, "2001:db8:feed::10", info.ClientIP)
	assert.Equal(t, "2001:db8:1::2", info.PeerIP)
	assert.Equal(t, "https", info.Scheme)
	assert.Equal(t, "pages.example", info.Host)
}

func TestResolveRequestInfoDoesNotMixForwardedProtoWithLegacyChain(t *testing.T) {
	policy, err := NewTrustedProxyPolicy([]string{"127.0.0.1/32"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Forwarded", `proto=https`)
	req.Header.Set("X-Forwarded-For", "198.51.100.10")
	req.Header.Set("X-Forwarded-Proto", "http")
	req.Header.Set("X-Forwarded-Host", "legacy.example")

	info := ResolveRequestInfo(req, policy)
	assert.Equal(t, "198.51.100.10", info.ClientIP)
	assert.Equal(t, "127.0.0.1", info.PeerIP)
	assert.Equal(t, "http", info.Scheme)
	assert.Equal(t, "legacy.example", info.Host)
}

func TestResolveRequestInfoMappedIPv6TrustedProxyPolicy(t *testing.T) {
	policy, err := NewTrustedProxyPolicy([]string{"::ffff:127.0.0.1/128"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
	req.RemoteAddr = "[::ffff:127.0.0.1]:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.10")
	req.Header.Set("X-Forwarded-Proto", "https")

	info := ResolveRequestInfo(req, policy)
	assert.Equal(t, "198.51.100.10", info.ClientIP)
	assert.Equal(t, "127.0.0.1", info.PeerIP)
	assert.Equal(t, "https", info.Scheme)
	assert.Equal(t, "example.com", info.Host)
}

func TestResolveRequestInfoTrustsIPv4PolicyForMappedIPv6Peer(t *testing.T) {
	policy, err := NewTrustedProxyPolicy([]string{"127.0.0.1/32"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
	req.RemoteAddr = "[::ffff:127.0.0.1]:1234"
	req.Header.Set("Forwarded", `for=198.51.100.10;proto=https`)

	info := ResolveRequestInfo(req, policy)
	assert.Equal(t, "198.51.100.10", info.ClientIP)
	assert.Equal(t, "127.0.0.1", info.PeerIP)
	assert.Equal(t, "https", info.Scheme)
	assert.Equal(t, "example.com", info.Host)
}

func TestResolveRequestInfoTrusts6to4ProxyChain(t *testing.T) {
	policy, err := NewTrustedProxyPolicy([]string{"2002:c633:6400::/40"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
	req.RemoteAddr = "[2002:c633:6401::2]:1234"
	req.Header.Set("Forwarded", `for=203.0.113.10, for="[2002:c633:6401::1]";proto=https`)

	info := ResolveRequestInfo(req, policy)
	assert.Equal(t, "203.0.113.10", info.ClientIP)
	assert.Equal(t, "2002:c633:6401::2", info.PeerIP)
	assert.Equal(t, "https", info.Scheme)
	assert.Equal(t, "example.com", info.Host)
}

func TestParseAddrSupportsIPv6Forms(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		want  string
		valid bool
	}{
		{name: "plain ipv6", raw: "2001:db8::1", want: "2001:db8::1", valid: true},
		{name: "bracketed ipv6", raw: "[2001:db8::1]", want: "2001:db8::1", valid: true},
		{name: "bracketed ipv6 with port", raw: "[2001:db8::1]:443", want: "2001:db8::1", valid: true},
		{name: "mapped ipv6", raw: "::ffff:127.0.0.1", want: "127.0.0.1", valid: true},
		{name: "zone ipv6", raw: "fe80::1%eth0", want: "fe80::1%eth0", valid: true},
		{name: "invalid forwarded token", raw: `"2001:db8::1"`, valid: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			addr, ok := parseAddr(tc.raw)
			assert.Equal(t, tc.valid, ok)
			if tc.valid {
				assert.Equal(t, tc.want, addr.String())
			}
		})
	}
}

func TestRequestInfoFromRequestPrefersInjectedContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
	req = req.WithContext(ContextWithRequestInfo(req.Context(), RequestInfo{
		ClientIP: "203.0.113.7",
		PeerIP:   "127.0.0.1",
		Scheme:   "https",
		Host:     "pages.example",
	}))

	info := RequestInfoFromRequest(req)
	assert.Equal(t, "203.0.113.7", info.ClientIP)
	assert.Equal(t, "https", info.Scheme)
	assert.Equal(t, "pages.example", info.Host)
}
