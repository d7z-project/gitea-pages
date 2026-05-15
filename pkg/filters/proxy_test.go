package filters

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func TestRewriteProxyRequestSanitizesForwardingAndDropsAuthorizationByDefault(t *testing.T) {
	target, err := url.Parse("https://upstream.example/base")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "https://pages.example/repo1/api/data?q=ok", nil)
	req.Host = "org1.example.com"
	req.RemoteAddr = "198.51.100.20:1234"
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Cookie", "session=secret")
	req.Header.Set("Proxy-Authorization", "Basic upstream")
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	req.Header.Set("X-Forwarded-Proto", "https")

	outReq := req.Clone(req.Context())
	pr := &httputil.ProxyRequest{In: req, Out: outReq}
	rewriteProxyRequest(pr, req, target, "/data", false, core.FilterContext{
		PageContent: &core.PageContent{Owner: "org1", Repo: "repo1", Path: "api/data"},
	})

	assert.Equal(t, "https", pr.Out.URL.Scheme)
	assert.Equal(t, "upstream.example", pr.Out.URL.Host)
	assert.Equal(t, "/base/data", pr.Out.URL.Path)
	assert.Equal(t, "q=ok", pr.Out.URL.RawQuery)
	assert.Empty(t, pr.Out.Header.Get("Authorization"))
	assert.Empty(t, pr.Out.Header.Get("Proxy-Authorization"))
	assert.Equal(t, "session=secret", pr.Out.Header.Get("Cookie"))
	assert.Equal(t, "198.51.100.20", pr.Out.Header.Get("X-Forwarded-For"))
	assert.Equal(t, `for=198.51.100.20;proto=https;host=org1.example.com`, pr.Out.Header.Get("Forwarded"))
	assert.Equal(t, "https", pr.Out.Header.Get("X-Forwarded-Proto"))
	assert.Equal(t, "org1.example.com", pr.Out.Header.Get("X-Forwarded-Host"))
	assert.Equal(t, "198.51.100.20", pr.Out.Header.Get("X-Real-IP"))
	assert.Equal(t, "198.51.100.20", pr.Out.Header.Get("X-Page-IP"))
	assert.Equal(t, "org1.example.com", pr.Out.Header.Get("X-Page-Host"))
}

func TestRewriteProxyRequestCanForwardAuthorization(t *testing.T) {
	target, err := url.Parse("https://upstream.example/base")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "https://pages.example/repo1/api/data?q=ok", nil)
	req.Host = "org1.example.com"
	req.RemoteAddr = "198.51.100.20:1234"
	req.Header.Set("Authorization", "Bearer secret")

	outReq := req.Clone(req.Context())
	pr := &httputil.ProxyRequest{In: req, Out: outReq}
	rewriteProxyRequest(pr, req, target, "/data", true, core.FilterContext{
		PageContent: &core.PageContent{Owner: "org1", Repo: "repo1", Path: "api/data"},
	})

	assert.Equal(t, "Bearer secret", pr.Out.Header.Get("Authorization"))
}

func TestRewriteProxyRequestTrustsConfiguredForwardedChain(t *testing.T) {
	target, err := url.Parse("https://upstream.example")
	require.NoError(t, err)
	policy, err := core.NewTrustedProxyPolicy([]string{"127.0.0.1/32", "10.0.0.0/8"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://pages.example/repo1/api", nil)
	req.Host = "org1.example.com"
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.10, 10.0.0.1")
	req.Header.Set("X-Forwarded-Proto", "https")
	req = req.WithContext(core.ContextWithRequestInfo(req.Context(), core.ResolveRequestInfo(req, policy)))

	outReq := req.Clone(req.Context())
	pr := &httputil.ProxyRequest{In: req, Out: outReq}
	rewriteProxyRequest(pr, req, target, "/", false, core.FilterContext{
		PageContent: &core.PageContent{Owner: "org1", Repo: "repo1", Path: "api"},
	})

	assert.Equal(t, "198.51.100.10", pr.Out.Header.Get("X-Forwarded-For"))
	assert.Equal(t, `for=198.51.100.10;proto=https;host=org1.example.com`, pr.Out.Header.Get("Forwarded"))
	assert.Equal(t, "https", pr.Out.Header.Get("X-Forwarded-Proto"))
	assert.Equal(t, "198.51.100.10", pr.Out.Header.Get("X-Real-IP"))
	assert.Equal(t, "198.51.100.10", pr.Out.Header.Get("X-Page-IP"))
}

func TestRewriteProxyRequestTrustsConfiguredIPv6ForwardedChain(t *testing.T) {
	target, err := url.Parse("https://upstream.example")
	require.NoError(t, err)
	policy, err := core.NewTrustedProxyPolicy([]string{"2001:db8:1::/48"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://pages.example/repo1/api", nil)
	req.Host = "org1.example.com"
	req.RemoteAddr = "[2001:db8:1::2]:1234"
	req.Header.Set("X-Forwarded-For", "[2001:db8:feed::10]:4321, 2001:db8:1::1")
	req.Header.Set("X-Forwarded-Proto", "https")
	req = req.WithContext(core.ContextWithRequestInfo(req.Context(), core.ResolveRequestInfo(req, policy)))

	outReq := req.Clone(req.Context())
	pr := &httputil.ProxyRequest{In: req, Out: outReq}
	rewriteProxyRequest(pr, req, target, "/", false, core.FilterContext{
		PageContent: &core.PageContent{Owner: "org1", Repo: "repo1", Path: "api"},
	})

	assert.Equal(t, "2001:db8:feed::10", pr.Out.Header.Get("X-Forwarded-For"))
	assert.Equal(t, `for="[2001:db8:feed::10]";proto=https;host=org1.example.com`, pr.Out.Header.Get("Forwarded"))
	assert.Equal(t, "https", pr.Out.Header.Get("X-Forwarded-Proto"))
	assert.Equal(t, "2001:db8:feed::10", pr.Out.Header.Get("X-Real-IP"))
	assert.Equal(t, "2001:db8:feed::10", pr.Out.Header.Get("X-Page-IP"))
}

func TestRewriteProxyRequestBuildsForwardedForMappedIPv6Peer(t *testing.T) {
	target, err := url.Parse("https://upstream.example")
	require.NoError(t, err)
	policy, err := core.NewTrustedProxyPolicy([]string{"127.0.0.1/32"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://pages.example/repo1/api", nil)
	req.Host = "org1.example.com"
	req.RemoteAddr = "[::ffff:127.0.0.1]:1234"
	req.Header.Set("Forwarded", `for=198.51.100.10;proto=https`)
	req = req.WithContext(core.ContextWithRequestInfo(req.Context(), core.ResolveRequestInfo(req, policy)))

	outReq := req.Clone(req.Context())
	pr := &httputil.ProxyRequest{In: req, Out: outReq}
	rewriteProxyRequest(pr, req, target, "/", false, core.FilterContext{
		PageContent: &core.PageContent{Owner: "org1", Repo: "repo1", Path: "api"},
	})

	assert.Equal(t, "198.51.100.10", pr.Out.Header.Get("X-Forwarded-For"))
	assert.Equal(t, `for=198.51.100.10;proto=https;host=org1.example.com`, pr.Out.Header.Get("Forwarded"))
	assert.Equal(t, "198.51.100.10", pr.Out.Header.Get("X-Real-IP"))
}

func TestParseProxyTargetRejectsInvalidInputs(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "http",
			raw:  "http://127.0.0.1:8080",
			want: "must use https",
		},
		{
			name: "userinfo",
			raw:  "https://user:pass@upstream.example",
			want: "must not include userinfo",
		},
		{
			name: "invalid port",
			raw:  "https://upstream.example:abc",
			want: "invalid port",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseProxyTarget(tc.raw)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestNewProxyPolicyRejectsInvalidDenyEntries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config proxyGlobalConfig
		want   string
	}{
		{
			name:   "deny host with port",
			config: proxyGlobalConfig{DenyHosts: []string{"metadata.internal.example:443"}},
			want:   "must not include a port or IP literal",
		},
		{
			name:   "deny host as ip",
			config: proxyGlobalConfig{DenyHosts: []string{"127.0.0.1"}},
			want:   "must be a hostname",
		},
		{
			name:   "invalid cidr",
			config: proxyGlobalConfig{DenyCIDRs: []string{"bad-cidr"}},
			want:   "deny_cidrs entry is invalid",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newProxyPolicy(tc.config)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestProxyPolicyTransportForTargetRejectsDeniedHost(t *testing.T) {
	policy, err := newProxyPolicy(proxyGlobalConfig{
		DenyHosts: []string{"metadata.internal.example"},
	})
	require.NoError(t, err)

	target, err := parseProxyTarget("https://metadata.internal.example")
	require.NoError(t, err)

	_, err = policy.transportForTarget(context.Background(), target)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is denied")
}

func TestProxyPolicyTransportForTargetRejectsLiteralDeniedIP(t *testing.T) {
	policy, err := newProxyPolicy(proxyGlobalConfig{
		DenyCIDRs: []string{"10.0.0.0/8"},
	})
	require.NoError(t, err)

	target, err := parseProxyTarget("https://10.1.2.3")
	require.NoError(t, err)

	_, err = policy.transportForTarget(context.Background(), target)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "denied by cidr rule")
}

func TestProxyPolicyTransportForTargetFiltersDeniedAddressesAndKeepsResolverOrder(t *testing.T) {
	policy, err := newProxyPolicy(proxyGlobalConfig{
		DenyCIDRs: []string{"10.0.0.0/8"},
	})
	require.NoError(t, err)

	target, err := parseProxyTarget("https://upstream.example")
	require.NoError(t, err)

	var attempts []string
	policy.resolver = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{
			netip.MustParseAddr("10.0.0.5"),
			netip.MustParseAddr("203.0.113.10"),
			netip.MustParseAddr("203.0.113.11"),
		}, nil
	}
	policy.dialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		attempts = append(attempts, addr)
		return nil, errors.New("dial failed")
	}

	transport, err := policy.transportForTarget(context.Background(), target)
	require.NoError(t, err)
	_, err = transport.DialContext(context.Background(), "tcp", "ignored:443")
	require.Error(t, err)
	assert.Equal(t, []string{"203.0.113.10:443", "203.0.113.11:443"}, attempts)
}

func TestProxyPolicyTransportForTargetFallsBackAcrossAllowedAddresses(t *testing.T) {
	policy, err := newProxyPolicy(proxyGlobalConfig{})
	require.NoError(t, err)

	target, err := parseProxyTarget("https://upstream.example")
	require.NoError(t, err)

	var attempts []string
	policy.resolver = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{
			netip.MustParseAddr("203.0.113.10"),
			netip.MustParseAddr("203.0.113.11"),
		}, nil
	}
	policy.dialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		attempts = append(attempts, addr)
		if len(attempts) == 1 {
			return nil, errors.New("first failed")
		}
		left, right := net.Pipe()
		go right.Close()
		return left, nil
	}

	transport, err := policy.transportForTarget(context.Background(), target)
	require.NoError(t, err)
	conn, err := transport.DialContext(context.Background(), "tcp", "ignored:443")
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	assert.Equal(t, []string{"203.0.113.10:443", "203.0.113.11:443"}, attempts)
}

func TestProxyPolicyTransportForTargetUsesOriginalHostnameForTLS(t *testing.T) {
	var (
		serverName string
		hostHeader string
	)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hostHeader = r.Host
		_, _ = w.Write([]byte("ok"))
	}))
	server.TLS = &tls.Config{
		GetConfigForClient: func(info *tls.ClientHelloInfo) (*tls.Config, error) {
			serverName = info.ServerName
			return nil, nil
		},
	}
	server.StartTLS()
	defer server.Close()

	listenerHost, port, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)
	resolvedAddr := netip.MustParseAddr(listenerHost)

	policy, err := newProxyPolicy(proxyGlobalConfig{})
	require.NoError(t, err)
	policy.resolver = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{resolvedAddr}, nil
	}

	target, err := parseProxyTarget("https://upstream.example:" + port + "/hello")
	require.NoError(t, err)
	transport, err := policy.transportForTarget(context.Background(), target)
	require.NoError(t, err)
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	req, err := http.NewRequest(http.MethodGet, target.String(), nil)
	require.NoError(t, err)
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, "upstream.example", serverName)
	assert.Equal(t, "upstream.example:"+port, hostHeader)
	assert.Equal(t, "ok", string(body))
}

func TestProxyPolicyTransportForTargetPreservesResolverOrderAfterFiltering(t *testing.T) {
	policy, err := newProxyPolicy(proxyGlobalConfig{
		DenyCIDRs: []string{"10.0.0.0/8", "192.168.0.0/16"},
	})
	require.NoError(t, err)

	target, err := parseProxyTarget("https://upstream.example:8443")
	require.NoError(t, err)

	policy.resolver = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{
			netip.MustParseAddr("10.0.0.5"),
			netip.MustParseAddr("203.0.113.10"),
			netip.MustParseAddr("192.168.1.9"),
			netip.MustParseAddr("203.0.113.11"),
		}, nil
	}

	var attempts []string
	policy.dialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		attempts = append(attempts, addr)
		return nil, errors.New("dial failed")
	}

	transport, err := policy.transportForTarget(context.Background(), target)
	require.NoError(t, err)
	_, err = transport.DialContext(context.Background(), "tcp", "ignored")
	require.Error(t, err)
	assert.True(t, slices.Equal(attempts, []string{"203.0.113.10:8443", "203.0.113.11:8443"}))
}
