package tests

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	testcore "gopkg.d7z.net/gitea-pages/tests/core"
)

func Test_GoJa_HandlerResponse(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function(request) {
  return new Response("512 + 512 = 1024", {
    status: 201,
    headers: { "X-Cache": "ignore" }
  })
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "api/v1/**"
  js:
    exec: "index.js"
`)

	data, _, err := server.OpenFile("https://org1.example.com/repo1/")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	data, resp, err := server.OpenFile("https://org1.example.com/repo1/api/v1/get")
	assert.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)
	assert.Equal(t, "ignore", resp.Header.Get("X-Cache"))
	assert.Equal(t, "512 + 512 = 1024", string(data))
}

func Test_GoJa_Request(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function(request) {
  return new Response(request.method + " " + new URL(request.url).pathname)
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "api/v1/**"
  js:
    exec: "index.js"
`)

	data, _, err := server.OpenFile("https://org1.example.com/repo1/api/v1/fetch")
	assert.NoError(t, err)
	assert.Equal(t, "GET /repo1/api/v1/fetch", string(data))

	data, _, err = server.OpenRequest(http.MethodPost, "https://org1.example.com/repo1/api/v1/fetch", nil)
	assert.NoError(t, err)
	assert.Equal(t, "POST /repo1/api/v1/fetch", string(data))
}

func Test_GoJa_RequestURLIsAbsoluteOnRealHTTP(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function(request) {
  const url = new URL(request.url)
  return Response.json({
    href: url.href,
    pathname: url.pathname,
    search: url.search,
  })
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "api/v1/**"
  js:
    exec: "index.js"
`)

	httpServer := server.StartHTTPServer("org1.example.com")
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/repo1/api/v1/fetch?q=ok")
	assert.NoError(t, err)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.JSONEq(t, `{"href":"http://org1.example.com/repo1/api/v1/fetch?q=ok","pathname":"/repo1/api/v1/fetch","search":"?q=ok"}`, string(body))
}

func Test_GoJa_RequestBody(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function(request) {
  const body = await request.text()
  return new Response(body)
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "api/v1/**"
  js:
    exec: "index.js"
`)

	data, _, err := server.OpenRequest(http.MethodPost, "https://org1.example.com/repo1/api/v1/fetch", bytes.NewBufferString("payload"))
	assert.NoError(t, err)
	assert.Equal(t, "payload", string(data))
}

func Test_GoJa_GiteaPagesFS(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function() {
  const root = fs.list()
  const docs = fs.list("docs")
  return Response.json({
    root: root.map(item => item.name).sort(),
    docs: docs.map(item => item.path).sort(),
  })
})
`)
	server.AddFile("org1/repo1/gh-pages/docs/a.txt", "a")
	server.AddFile("org1/repo1/gh-pages/docs/b.txt", "b")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "api/v1/**"
  js:
    exec: "index.js"
`)

	data, _, err := server.OpenFile("https://org1.example.com/repo1/api/v1/list")
	assert.NoError(t, err)
	assert.JSONEq(t, `{"root":[".pages.yaml","docs","index.html","index.js"],"docs":["docs/a.txt","docs/b.txt"]}`, string(data))
}

func Test_GoJa_HostAuth(t *testing.T) {
	server := newAuthTestServer(t, &fakeAuthProvider{
		session:    authSession("u1", "dragon"),
		authorized: true,
	})
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function() {
  return Response.json({
    authenticated: page.auth.authenticated,
    subject: page.auth.identity.subject,
    name: page.auth.identity.name,
  })
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
private: true
routes:
- path: "api/v1/**"
  js:
    exec: "index.js"
`)

	loginThroughAuth(t, server, "/repo1/api/v1/whoami")
	data, _, err := server.OpenFile("https://org1.example.com/repo1/api/v1/whoami")
	assert.NoError(t, err)
	assert.JSONEq(t, `{"authenticated":true,"subject":"u1","name":"dragon"}`, string(data))
}

func Test_GoJa_Async(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function() {
  await new Promise(resolve => setTimeout(resolve, 50))
  return new Response("abc")
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "**"
  js:
    exec: "index.js"
`)

	data, _, err := server.OpenFile("https://org1.example.com/repo1/api/v1/fetch")
	assert.NoError(t, err)
	assert.Equal(t, "abc", string(data))
}

func Test_GoJa_CancelPendingPromise(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function() {
  await new Promise(() => {})
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "**"
  js:
    exec: "index.js"
`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		_, _, err := server.OpenRequestWithContext(ctx, http.MethodGet, "https://org1.example.com/repo1/api/v1/pending", nil)
		done <- err
	}()

	select {
	case err := <-done:
		assert.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("request did not stop after context cancellation")
	}
}

func Test_GoJa_CancelledRequestDoesNotFallBackToNotFound(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function() {
  await new Promise(resolve => setTimeout(resolve, 100))
  return new Response("ok")
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "**"
  js:
    exec: "index.js"
`)

	httpServer := server.StartHTTPServer("org1.example.com")
	defer httpServer.Close()

	client := &http.Client{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/repo1/cancel-check", nil)
	assert.NoError(t, err)

	resp, err := client.Do(req)
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func Test_GoJa_Fetch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "test-header")
		_, _ = w.Write([]byte("fetched-content"))
	}))
	defer ts.Close()

	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function() {
  const res = await fetch('%s')
  return new Response(await res.text(), {
    headers: { "X-Fetched-Header": res.headers.get("X-Test") || res.headers.get("x-test") }
  })
})
`, ts.URL)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "**"
  js:
    exec: "index.js"
`)

	data, resp, err := server.OpenFile("https://org1.example.com/repo1/fetch")
	assert.NoError(t, err)
	assert.Equal(t, "fetched-content", string(data))
	assert.Equal(t, "test-header", resp.Header.Get("X-Fetched-Header"))
}

func Test_GoJa_FrameworkHelpers(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
const app = http.router()

app.get("/repo1/api/v1/users/:id", async (request, ctx) => {
  return http.json({
    id: ctx.params.id,
    page: page.meta.repo,
    q: ctx.query.get("q"),
    method: request.method,
  })
})

app.get("/repo1/api/v1/plain", async () => {
  return http.text("plain")
})

serve(app)
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "api/v1/**"
  js:
    exec: "index.js"
`)

	data, resp, err := server.OpenFile("https://org1.example.com/repo1/api/v1/users/42?q=ok")
	assert.NoError(t, err)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	assert.JSONEq(t, `{"id":"42","page":"repo1","q":"ok","method":"GET"}`, string(data))

	data, resp, err = server.OpenFile("https://org1.example.com/repo1/api/v1/plain")
	assert.NoError(t, err)
	assert.Equal(t, "text/plain; charset=utf-8", resp.Header.Get("Content-Type"))
	assert.Equal(t, "plain", string(data))
}

func Test_GoJa_FrameworkReadAndCors(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
const app = http.router()

app.post("/repo1/api/v1/json", async (request) => {
  const body = await http.read(request, "json")
  return await http.cors(http.json(body), {
    origin: "https://example.com",
    credentials: true,
  })
})

app.post("/repo1/api/v1/form", async (request) => {
  const body = await http.read(request, "form")
  return await http.withHeaders(http.json(body), {
    "x-mode": "form",
  })
})

serve(app)
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "api/v1/**"
  js:
    exec: "index.js"
`)

	data, resp, err := server.OpenRequest(
		http.MethodPost,
		"https://org1.example.com/repo1/api/v1/json",
		bytes.NewBufferString(`{"ok":true}`),
	)
	assert.NoError(t, err)
	assert.Equal(t, "https://example.com", resp.Header.Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", resp.Header.Get("Access-Control-Allow-Credentials"))
	assert.JSONEq(t, `{"ok":true}`, string(data))

	data, resp, err = server.OpenRequest(
		http.MethodPost,
		"https://org1.example.com/repo1/api/v1/form",
		bytes.NewBufferString("name=dragon&lang=go"),
	)
	assert.NoError(t, err)
	assert.Equal(t, "form", resp.Header.Get("X-Mode"))
	assert.JSONEq(t, `{"name":"dragon","lang":"go"}`, string(data))
}

func Test_GoJa_FrameworkCookiesAndStatusHelpers(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
const app = http.router()

app.get("/repo1/api/v1/cookie", async (request) => {
  const token = http.cookie(request, "token")
  return await http.setCookie(http.json({ token }), "session", "abc", {
    path: "/",
    httpOnly: true,
    sameSite: "Lax",
  })
})

app.get("/repo1/api/v1/empty", async () => {
  return http.noContent()
})

app.get("/repo1/api/v1/clear", async () => {
  return await http.clearCookie(http.text("bye"), "session", { path: "/" })
})

app.get("/repo1/api/v1/items/:id", async (request, ctx) => {
  return http.text("item:" + ctx.params.id)
})

serve(app)
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "api/v1/**"
  js:
    exec: "index.js"
`)

	_, _, err := server.OpenRequest(http.MethodGet, "https://org1.example.com/repo1/api/v1/cookie", nil)
	assert.NoError(t, err)

	data, resp, err := server.OpenRequest(http.MethodGet, "https://org1.example.com/repo1/api/v1/cookie", nil)
	assert.NoError(t, err)
	assert.Contains(t, resp.Header.Get("Set-Cookie"), "session=abc")
	assert.JSONEq(t, `{"token":null}`, string(data))

	data, resp, err = server.OpenRequest(http.MethodGet, "https://org1.example.com/repo1/api/v1/empty", nil)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Empty(t, string(data))

	data, resp, err = server.OpenRequest(http.MethodGet, "https://org1.example.com/repo1/api/v1/clear", nil)
	assert.NoError(t, err)
	assert.Contains(t, resp.Header.Get("Set-Cookie"), "session=")
	assert.Contains(t, resp.Header.Get("Set-Cookie"), "Max-Age=0")
	assert.Equal(t, "bye", string(data))

	_, resp, err = server.OpenRequest(http.MethodPost, "https://org1.example.com/repo1/api/v1/items/42", nil)
	assert.Error(t, err)
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	assert.Equal(t, "GET", resp.Header.Get("Allow"))
}

func Test_GoJa_WebSocket(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(function(request) {
  const { socket, response } = upgradeWebSocket(request)
  socket.addEventListener("message", async (event) => {
    await socket.send("ECHO: " + event.data)
    socket.close()
  })
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
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	assert.NoError(t, err)
	if err != nil {
		return
	}
	defer conn.Close()

	assert.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("hello")))
	messageType, payload, err := conn.ReadMessage()
	assert.NoError(t, err)
	assert.Equal(t, websocket.TextMessage, messageType)
	assert.Equal(t, "ECHO: hello", string(payload))
}

func Test_GoJa_SSE(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(function() {
  const { stream, response } = http.sse()
  ;(async () => {
    await stream.send(JSON.stringify({ ok: true }), { event: "message", id: "1" })
    stream.close()
  })()
  return response
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "sse"
  js:
    exec: "index.js"
`)

	httpServer := server.StartHTTPServer("org1.example.com")
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/repo1/sse")
	assert.NoError(t, err)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	assert.Contains(t, resp.Header.Get("Content-Type"), "text/event-stream")

	reader := bufio.NewReader(resp.Body)
	line1, _ := reader.ReadString('\n')
	line2, _ := reader.ReadString('\n')
	line3, _ := reader.ReadString('\n')
	assert.Equal(t, "event: message\n", line1)
	assert.Equal(t, "id: 1\n", line2)
	assert.Equal(t, "data: {\"ok\":true}\n", line3)
}

func Test_GoJa_StressConcurrentSimpleHTTP(t *testing.T) {
	if os.Getenv("GOJA_STRESS") != "1" {
		t.Skip("set GOJA_STRESS=1 to run goja stress tests")
	}
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function(request) {
  await new Promise(resolve => setTimeout(resolve, 1))
  return Response.json({
    ok: true,
    path: new URL(request.url).pathname,
  })
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "**"
  js:
    exec: "index.js"
`)

	httpServer := server.StartHTTPServer("org1.example.com")
	defer httpServer.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	const concurrency = 64
	const rounds = 8

	errCh := make(chan error, concurrency*rounds)
	var wg sync.WaitGroup
	for i := 0; i < concurrency*rounds; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := client.Get(httpServer.URL + "/repo1/stress-" + strconv.Itoa(i))
			if err != nil {
				errCh <- err
				return
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				errCh <- err
				return
			}
			if resp.StatusCode != http.StatusOK {
				errCh <- assert.AnError
				t.Logf("unexpected status=%d body=%s", resp.StatusCode, string(body))
				return
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent simple HTTP requests did not complete in time")
	}
	close(errCh)
	for err := range errCh {
		assert.NoError(t, err)
	}
}

func Test_GoJa_StressConcurrentCancelledHTTP(t *testing.T) {
	if os.Getenv("GOJA_STRESS") != "1" {
		t.Skip("set GOJA_STRESS=1 to run goja stress tests")
	}
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function() {
  await new Promise(resolve => setTimeout(resolve, 50))
  return new Response("ok")
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "**"
  js:
    exec: "index.js"
`)

	httpServer := server.StartHTTPServer("org1.example.com")
	defer httpServer.Close()

	client := &http.Client{}
	const concurrency = 64

	errCh := make(chan error, concurrency)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/repo1/cancel-"+strconv.Itoa(i), nil)
			if err != nil {
				errCh <- err
				return
			}
			resp, err := client.Do(req)
			if resp != nil && resp.Body != nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
			if err == nil {
				errCh <- assert.AnError
				t.Logf("expected cancellation for request %d", i)
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent cancelled HTTP requests did not complete in time")
	}
	close(errCh)
	for err := range errCh {
		assert.NoError(t, err)
	}
}
