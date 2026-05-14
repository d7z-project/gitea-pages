package core

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRequestInfoIgnoresUntrustedForwardedHeaders(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.RemoteAddr = "198.51.100.20:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	req.Header.Set("X-Forwarded-Proto", "https")

	info := ResolveRequestInfo(req, nil)
	assert.Equal(t, "198.51.100.20", info.ClientIP)
	assert.Equal(t, "198.51.100.20", info.PeerIP)
	assert.Equal(t, "http", info.Scheme)
	assert.False(t, info.TrustedProxy)
}

func TestResolveRequestInfoTrustsConfiguredProxyChain(t *testing.T) {
	policy, err := NewTrustedProxyPolicy([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.RemoteAddr = "10.0.0.2:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.10, 10.0.0.1")
	req.Header.Set("X-Forwarded-Proto", "https")

	info := ResolveRequestInfo(req, policy)
	assert.Equal(t, "198.51.100.10", info.ClientIP)
	assert.Equal(t, "10.0.0.2", info.PeerIP)
	assert.Equal(t, "https", info.Scheme)
	assert.True(t, info.TrustedProxy)
}

func TestRequestInfoFromRequestPrefersInjectedContext(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req = req.WithContext(ContextWithRequestInfo(req.Context(), RequestInfo{
		ClientIP:     "203.0.113.7",
		PeerIP:       "127.0.0.1",
		Scheme:       "https",
		TrustedProxy: true,
	}))

	info := RequestInfoFromRequest(req)
	assert.Equal(t, "203.0.113.7", info.ClientIP)
	assert.Equal(t, "https", info.Scheme)
	assert.True(t, info.TrustedProxy)
}
