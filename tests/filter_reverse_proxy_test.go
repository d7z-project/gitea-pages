package tests

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	testcore "gopkg.d7z.net/gitea-pages/tests/core"
)

func Test_Filter_ReverseProxyForwardsRequestToConfiguredUpstream(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{
			"method":        r.Method,
			"path":          r.URL.Path,
			"query":         r.URL.RawQuery,
			"authorization": r.Header.Get("Authorization"),
			"cookie":        r.Header.Get("Cookie"),
			"x_real_ip":     r.Header.Get("X-Real-IP"),
			"x_page_ip":     r.Header.Get("X-Page-IP"),
			"x_page_host":   r.Header.Get("X-Page-Host"),
			"x_page_refer":  r.Header.Get("X-Page-Refer"),
			"x_forwarded":   r.Header.Get("X-Forwarded-For"),
			"forwarded":     r.Header.Get("Forwarded"),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer upstream.Close()

	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots}
	originalTransport := http.DefaultTransport
	http.DefaultTransport = transport
	defer func() {
		http.DefaultTransport = originalTransport
	}()

	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "home")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "proxy/**"
  reverse_proxy:
    prefix: "/proxy"
    target: %q
`, upstream.URL+"/base")

	req := httptest.NewRequest(http.MethodGet, "https://org1.example.com/repo1/proxy/echo?q=1", nil)
	req.Host = "org1.example.com"
	req.RemoteAddr = "198.51.100.25:1234"
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Cookie", "session=demo")

	data, resp, err := server.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `{
		"method":"GET",
		"path":"/base/echo",
		"query":"q=1",
		"authorization":"",
		"cookie":"session=demo",
		"x_real_ip":"198.51.100.25",
		"x_page_ip":"198.51.100.25",
		"x_page_host":"org1.example.com",
		"x_page_refer":"org1/repo1/proxy/echo",
		"x_forwarded":"198.51.100.25",
		"forwarded":"for=198.51.100.25;proto=https;host=org1.example.com"
	}`, string(data))
}
