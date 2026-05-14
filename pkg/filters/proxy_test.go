package filters

import (
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func TestRewriteProxyRequestStripsSensitiveHeadersAndRebuildsForwarding(t *testing.T) {
	target, err := url.Parse("https://upstream.example/base")
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "https://pages.example/repo1/api/data?q=ok", nil)
	req.Host = "org1.example.com"
	req.RemoteAddr = "198.51.100.20:1234"
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Cookie", "session=secret")
	req.Header.Set("X-Test-Strip", "drop-me")
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	req.Header.Set("X-Forwarded-Proto", "https")

	outReq := req.Clone(req.Context())
	pr := &httputil.ProxyRequest{In: req, Out: outReq}
	rewriteProxyRequest(pr, req, target, "/data", []string{"Authorization", "Cookie", "X-Test-Strip"}, core.FilterContext{
		PageContent: &core.PageContent{Owner: "org1", Repo: "repo1", Path: "api/data"},
	})

	assert.Equal(t, "https", pr.Out.URL.Scheme)
	assert.Equal(t, "upstream.example", pr.Out.URL.Host)
	assert.Equal(t, "/base/data", pr.Out.URL.Path)
	assert.Equal(t, "q=ok", pr.Out.URL.RawQuery)
	assert.Empty(t, pr.Out.Header.Get("Authorization"))
	assert.Empty(t, pr.Out.Header.Get("Cookie"))
	assert.Empty(t, pr.Out.Header.Get("X-Test-Strip"))
	assert.Equal(t, "198.51.100.20", pr.Out.Header.Get("X-Forwarded-For"))
	assert.Equal(t, "https", pr.Out.Header.Get("X-Forwarded-Proto"))
	assert.Equal(t, "org1.example.com", pr.Out.Header.Get("X-Forwarded-Host"))
	assert.Equal(t, "198.51.100.20", pr.Out.Header.Get("X-Real-IP"))
	assert.Equal(t, "198.51.100.20", pr.Out.Header.Get("X-Page-IP"))
	assert.Equal(t, "org1.example.com", pr.Out.Header.Get("X-Page-Host"))
}

func TestRewriteProxyRequestTrustsConfiguredForwardedChain(t *testing.T) {
	target, err := url.Parse("https://upstream.example")
	require.NoError(t, err)
	policy, err := core.NewTrustedProxyPolicy([]string{"127.0.0.1/32", "10.0.0.0/8"})
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "http://pages.example/repo1/api", nil)
	req.Host = "org1.example.com"
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.10, 10.0.0.1")
	req.Header.Set("X-Forwarded-Proto", "https")
	req = req.WithContext(core.ContextWithRequestInfo(req.Context(), core.ResolveRequestInfo(req, policy)))

	outReq := req.Clone(req.Context())
	pr := &httputil.ProxyRequest{In: req, Out: outReq}
	rewriteProxyRequest(pr, req, target, "/", defaultProxyStripHeaders, core.FilterContext{
		PageContent: &core.PageContent{Owner: "org1", Repo: "repo1", Path: "api"},
	})

	assert.Equal(t, "198.51.100.10, 10.0.0.1, 127.0.0.1", pr.Out.Header.Get("X-Forwarded-For"))
	assert.Equal(t, "https", pr.Out.Header.Get("X-Forwarded-Proto"))
	assert.Equal(t, "198.51.100.10", pr.Out.Header.Get("X-Real-IP"))
	assert.Equal(t, "198.51.100.10", pr.Out.Header.Get("X-Page-IP"))
}

func TestParseProxyTargetRequiresHTTPS(t *testing.T) {
	_, err := parseProxyTarget("http://127.0.0.1:8080")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must use https")
}
