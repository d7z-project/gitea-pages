package tests

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	testcore "gopkg.d7z.net/gitea-pages/tests/core"
)

func Test_Security_DefaultRejectsCrossOrigin(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")

	req := httptest.NewRequest(http.MethodGet, "https://org1.example.com/repo1/", nil)
	req.Header.Set("Origin", "https://app.example.com")

	_, resp, err := server.Do(req)
	assert.Error(t, err)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func Test_Security_PageCORSAllowlistHandlesPreflight(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
security:
  cors:
    origins:
      - https://app.example.com
`)

	req := httptest.NewRequest(http.MethodOptions, "https://org1.example.com/repo1/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")

	_, resp, err := server.Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, "https://app.example.com", resp.Header.Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, POST, PUT, PATCH, DELETE, OPTIONS", resp.Header.Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "same-origin", resp.Header.Get("Cross-Origin-Resource-Policy"))
}

func Test_Security_HTTPCookiesAreStripped(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function(request) {
  return await http.setCookie(http.json({ token: http.cookie(request, "token") }), "session", "abc", {
    path: "/",
    httpOnly: true,
  })
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "api/**"
  js:
    exec: "index.js"
`)

	req := httptest.NewRequest(http.MethodGet, "http://org1.example.com/repo1/api/test", nil)
	req.Header.Set("Cookie", "token=secret")

	data, resp, err := server.Do(req)
	assert.NoError(t, err)
	assert.Empty(t, resp.Header.Values("Set-Cookie"))
	assert.JSONEq(t, `{"token":null}`, string(data))
}

func Test_Security_HTTPSSameOriginCookiesStayEnabled(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function(request) {
  return await http.setCookie(http.json({ token: http.cookie(request, "token") }), "session", "abc", {
    path: "/",
    httpOnly: true,
  })
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "api/**"
  js:
    exec: "index.js"
`)

	req := httptest.NewRequest(http.MethodGet, "https://org1.example.com/repo1/api/test", nil)
	req.Header.Set("Cookie", "token=secret")

	data, resp, err := server.Do(req)
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.Header.Values("Set-Cookie"))
	assert.JSONEq(t, `{"token":"secret"}`, string(data))
}

func Test_Security_RejectsCrossOriginBeforeProxyRoute(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
security:
  cors:
    origins: []
routes:
- path: "proxy/**"
  reverse_proxy:
    prefix: "/proxy"
    target: "https://example.invalid"
`)

	req := httptest.NewRequest(http.MethodGet, "https://org1.example.com/repo1/proxy/test", nil)
	req.Header.Set("Origin", "https://app.example.com")

	_, resp, err := server.Do(req)
	assert.Error(t, err)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func Test_Security_WebSocketCrossOriginRejected(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(function(request) {
  const { response } = upgradeWebSocket(request)
  return response
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "ws"
  js:
    exec: "index.js"
`)

	httpServer := server.StartHTTPServer("org1.example.com")
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/repo1/ws"
	header := http.Header{"Origin": []string{"https://app.example.com"}}
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	assert.Error(t, err)
	if resp != nil {
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	}
}

func Test_Security_PageCORSAllowsCrossOriginResponse(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function() {
  return Response.json({ ok: true })
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
security:
  cors:
    origins:
      - https://app.example.com
    credentials: true
routes:
- path: "api/**"
  js:
    exec: "index.js"
`)

	req := httptest.NewRequest(http.MethodPost, "https://org1.example.com/repo1/api/test", bytes.NewBufferString(`{}`))
	req.Header.Set("Origin", "https://app.example.com")

	_, resp, err := server.Do(req)
	assert.NoError(t, err)
	assert.Equal(t, "https://app.example.com", resp.Header.Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", resp.Header.Get("Access-Control-Allow-Credentials"))
}
