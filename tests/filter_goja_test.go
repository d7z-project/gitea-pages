package tests

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/gitea-pages/pkg"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	testcore "gopkg.d7z.net/gitea-pages/tests/core"
	"gopkg.d7z.net/middleware/kv"
	mwstorage "gopkg.d7z.net/middleware/storage"
)

func newGoJaTestServer(script string, routePath ...string) *testcore.TestServer {
	return newGoJaTestServerWithFilterConfig(script, nil, routePath...)
}

func newGoJaTestServerWithOptions(script string, options []pkg.ServerOption, routePath ...string) *testcore.TestServer {
	server := testcore.NewTestServerOptions("example.com", options...)
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", "%s", script)
	path := "**"
	if len(routePath) > 0 && routePath[0] != "" {
		path = routePath[0]
	}
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: %q
  js:
    exec: "index.js"
`, path)
	return server
}

func newGoJaTestServerWithFilterConfig(script string, filterConfig map[string]map[string]any, routePath ...string) *testcore.TestServer {
	options := make([]pkg.ServerOption, 0, 1)
	if filterConfig != nil {
		options = append(options, pkg.WithFilterConfig(filterConfig))
	}
	return newGoJaTestServerWithOptions(script, options, routePath...)
}

func addGoJaRepo(server *testcore.TestServer, repo, routePath, script string) {
	base := "org1/" + repo + "/gh-pages/"
	server.AddFile(base+"index.html", "hello world")
	server.AddFile(base+"index.js", "%s", script)
	server.AddFile(base+".pages.yaml", `
routes:
- path: %q
  js:
    exec: "index.js"
`, routePath)
}

func Test_GoJa_HandlerResponse(t *testing.T) {
	server := newGoJaTestServer(`
serve(async function(request) {
  return new Response("512 + 512 = 1024", {
    status: 201,
    headers: { "X-Cache": "ignore" }
  })
})
`, "api/v1/**")
	defer server.Close()

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
	server := newGoJaTestServer(`
serve(async function(request) {
  return new Response(request.method + " " + new URL(request.url).pathname)
})
`, "api/v1/**")
	defer server.Close()

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
	if !assert.NoError(t, err) {
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.JSONEq(t, `{"href":"http://org1.example.com/repo1/api/v1/fetch?q=ok","pathname":"/repo1/api/v1/fetch","search":"?q=ok"}`, string(body))
}

func Test_GoJa_RequestIP(t *testing.T) {
	server := testcore.NewTestServerOptions("example.com", pkg.WithTrustedProxies([]string{"127.0.0.1/32", "10.0.0.0/8"}))
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `%s`, `
serve(async function(request) {
  return Response.json({
    ip: request.ip,
    remoteIP: request.RemoteIP,
  })
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "api/v1/**"
  js:
    exec: "index.js"
`)
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet, "http://org1.example.com/repo1/api/v1/fetch", nil)
	req.Host = "org1.example.com"
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.10, 10.0.0.1")
	respBody, resp, err := server.Do(req)
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, 200, resp.StatusCode)
	assert.JSONEq(t, `{"ip":"198.51.100.10","remoteIP":"198.51.100.10"}`, string(respBody))
}

func Test_GoJa_RequestBody(t *testing.T) {
	server := newGoJaTestServer(`
serve(async function(request) {
  const body = await request.text()
  return new Response(body)
})
`, "api/v1/**")
	defer server.Close()

	data, _, err := server.OpenRequest(http.MethodPost, "https://org1.example.com/repo1/api/v1/fetch", bytes.NewBufferString("payload"))
	assert.NoError(t, err)
	assert.Equal(t, "payload", string(data))
}

func Test_GoJa_RequestBodyDefaultLimit(t *testing.T) {
	server := newGoJaTestServer(`
serve(async function(request) {
  return new Response(await request.text())
})
`, "api/v1/**")
	defer server.Close()

	httpServer := server.StartHTTPServer("org1.example.com")
	defer httpServer.Close()

	resp, err := http.Post(httpServer.URL+"/repo1/api/v1/fetch", "text/plain", strings.NewReader(strings.Repeat("a", (4<<20)+1)))
	if !assert.NoError(t, err) {
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Contains(t, string(body), "request body exceeds limit: 4194304")
}

func Test_GoJa_RequestBodyServerLimit(t *testing.T) {
	server := newGoJaTestServerWithOptions(`
serve(async function(request) {
  return new Response(await request.text())
})
`, []pkg.ServerOption{
		pkg.WithFilterServerConfig(core.FilterServerConfig{MaxRequestBodyBytes: 8}),
	}, "api/v1/**")
	defer server.Close()

	httpServer := server.StartHTTPServer("org1.example.com")
	defer httpServer.Close()

	resp, err := http.Post(httpServer.URL+"/repo1/api/v1/fetch", "text/plain", strings.NewReader("123456789"))
	if !assert.NoError(t, err) {
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Contains(t, string(body), "request body exceeds limit: 8")
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

func Test_GoJa_GiteaPagesFSRead(t *testing.T) {
	server := newGoJaTestServer(`
serve(async function() {
  const syncText = fs.readTextSync("docs/a.txt")
  const asyncText = await fs.readText("docs/b.txt")
  const syncBytes = new TextDecoder().decode(fs.readSync("docs/a.txt"))
  const asyncBytes = new TextDecoder().decode(await fs.read("docs/b.txt"))
  return Response.json({
    syncText,
    asyncText,
    syncBytes,
    asyncBytes,
  })
})
`, "api/v1/**")
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/docs/a.txt", "alpha")
	server.AddFile("org1/repo1/gh-pages/docs/b.txt", "beta")

	data, _, err := server.OpenFile("https://org1.example.com/repo1/api/v1/io")
	assert.NoError(t, err)
	assert.JSONEq(t, `{"syncText":"alpha","asyncText":"beta","syncBytes":"alpha","asyncBytes":"beta"}`, string(data))
}

func Test_GoJa_GiteaPagesFSOpenReadable(t *testing.T) {
	server := newGoJaTestServer(`
serve(async function() {
  const reader = fs.openReadable("docs/a.txt")
  const first = await reader.read({ size: 2 })
  const second = await reader.read({ size: 8 })
  const done = await reader.read()
  return Response.json({
    first: new TextDecoder().decode(first.value),
    second: new TextDecoder().decode(second.value),
    done: done.done,
  })
})
`, "api/v1/**")
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/docs/a.txt", "alpha")

	data, _, err := server.OpenFile("https://org1.example.com/repo1/api/v1/stream")
	assert.NoError(t, err)
	assert.JSONEq(t, `{"first":"al","second":"pha","done":true}`, string(data))
}

func Test_GoJa_StorageReadWriteAndDirOps(t *testing.T) {
	store := mwstorage.NewMemoryStorage()
	server := newGoJaTestServerWithOptions(`
serve(async function() {
  await storage.mkdir("docs", { recursive: true })
  await storage.writeFile("docs/a.txt", "a")
  storage.writeFileSync("docs/b.txt", "b")
  const writer = storage.openWritable("docs/stream.txt")
  await writer.write("st")
  await writer.write("ream")
  await writer.close()
  const reader = storage.openReadable("docs/stream.txt", { offset: 2 })
  const streamed = await reader.read({ size: 8 })
  await reader.close()
  const nested = storage.child("nested")
  await nested.writeFile("c.txt", "c", { mkdir: true })
  await storage.copyFile("docs/a.txt", "docs/copy.txt")
  await storage.rename("docs/copy.txt", "docs/final.txt")

  const text = await storage.readFile("docs/a.txt", "utf8")
  const syncText = storage.readFileSync("docs/b.txt", "utf8")
  const entries = await storage.readdir("docs", { withFileTypes: true })
  const names = storage.readdirSync("docs").slice().sort()
  const recursive = storage.readdirSync(".", { recursive: true }).slice().sort()
  const stat = await storage.stat("docs/final.txt")

  return Response.json({
    text,
    syncText,
    streamed: new TextDecoder().decode(streamed.value),
    names,
    recursive,
    entryKinds: entries.map(item => ({ name: item.name, file: item.isFile(), dir: item.isDirectory() })).sort((a, b) => a.name.localeCompare(b.name)),
    stat: {
      path: stat.path,
      file: stat.isFile(),
      dir: stat.isDirectory(),
      size: stat.size
    }
  })
})
`, []pkg.ServerOption{pkg.WithStorage(store)}, "api/v1/**")
	defer server.Close()

	data, _, err := server.OpenFile("https://org1.example.com/repo1/api/v1/storage")
	assert.NoError(t, err)
	assert.JSONEq(t, `{
		"text":"a",
		"syncText":"b",
		"streamed":"ream",
		"names":["a.txt","b.txt","final.txt","stream.txt"],
		"recursive":["docs","docs/a.txt","docs/b.txt","docs/final.txt","docs/stream.txt","nested","nested/c.txt"],
		"entryKinds":[
			{"name":"a.txt","file":true,"dir":false},
			{"name":"b.txt","file":true,"dir":false},
			{"name":"final.txt","file":true,"dir":false},
			{"name":"stream.txt","file":true,"dir":false}
		],
		"stat":{"path":"docs/final.txt","file":true,"dir":false,"size":1}
	}`, string(data))

	file, err := store.Child("repo", "org1", "repo1").Open("nested/c.txt")
	assert.NoError(t, err)
	defer file.Close()
	fileData, readErr := io.ReadAll(file)
	assert.NoError(t, readErr)
	assert.Equal(t, "c", string(fileData))
}

func Test_GoJa_StorageIsolationBetweenRepos(t *testing.T) {
	store := mwstorage.NewMemoryStorage()
	server := testcore.NewTestServerOptions("example.com", pkg.WithStorage(store))
	defer server.Close()
	addGoJaRepo(server, "repo1", "api/**", `
serve(async function() {
  await storage.writeFile("shared.txt", "repo1")
  return http.json({ ok: true })
})
`)
	addGoJaRepo(server, "repo2", "api/**", `
serve(async function() {
  return http.json({
    exists: storage.existsSync("shared.txt"),
    root: storage.readdirSync()
  })
})
`)

	_, _, err := server.OpenFile("https://org1.example.com/repo1/api/write")
	assert.NoError(t, err)

	data, _, err := server.OpenFile("https://org1.example.com/repo2/api/check")
	assert.NoError(t, err)
	assert.JSONEq(t, `{"exists":false,"root":[]}`, string(data))
}

func Test_GoJa_StorageRejectsPathEscape(t *testing.T) {
	server := newGoJaTestServer(`
serve(async function() {
  const errors = []
  try {
    await storage.readFile("../bad.txt", "utf8")
  } catch (err) {
    errors.push(String(err))
  }
  try {
    storage.child("..").writeFileSync("x.txt", "blocked")
  } catch (err) {
    errors.push(String(err))
  }
  return Response.json({ errors })
})
`, "api/v1/**")
	defer server.Close()

	data, _, err := server.OpenFile("https://org1.example.com/repo1/api/v1/escape")
	assert.NoError(t, err)
	assert.Contains(t, string(data), "storage: invalid child path")
}

func Test_GoJa_StorageRemoveAndExists(t *testing.T) {
	server := newGoJaTestServer(`
serve(async function() {
  await storage.writeFile("tmp/a.txt", "1", { mkdir: true })
  await storage.writeFile("tmp/b.txt", "2", { mkdir: true })
  await storage.rm("tmp/a.txt")
  storage.rmSync("tmp/b.txt")
  await storage.writeFile("tmp/c.txt", "3", { mkdir: true })
  storage.unlinkSync("tmp/c.txt")
  return Response.json({
    a: await storage.exists("tmp/a.txt"),
    b: storage.existsSync("tmp/b.txt"),
    c: storage.existsSync("tmp/c.txt")
  })
})
`, "api/v1/**")
	defer server.Close()

	data, _, err := server.OpenFile("https://org1.example.com/repo1/api/v1/remove")
	assert.NoError(t, err)
	assert.JSONEq(t, `{"a":false,"b":false,"c":false}`, string(data))
}

func Test_GoJa_StorageExample(t *testing.T) {
	server := newGoJaTestServer(`
serve(async function(request) {
  const pathname = new URL(request.url).pathname

  if (pathname.endsWith("/write")) {
    const current = await storage.exists("hello.txt")
      ? await storage.readFile("hello.txt", "utf8")
      : ""
    const next = current ? current + "\nupdated" : "created"
    await storage.writeFile("hello.txt", next)
  }

  const exists = await storage.exists("hello.txt")
  const content = exists ? await storage.readFile("hello.txt", "utf8") : ""
  const files = exists ? storage.readdirSync().sort() : []

  return Response.json({
    exists,
    content,
    files,
  })
})
`, "storage*")
	defer server.Close()

	data, _, err := server.OpenFile("https://org1.example.com/repo1/storage")
	assert.NoError(t, err)
	assert.JSONEq(t, `{"exists":false,"content":"","files":[]}`, string(data))

	data, _, err = server.OpenFile("https://org1.example.com/repo1/storage/write")
	assert.NoError(t, err)
	assert.JSONEq(t, `{"exists":true,"content":"created","files":["hello.txt"]}`, string(data))

	data, _, err = server.OpenFile("https://org1.example.com/repo1/storage/write")
	assert.NoError(t, err)
	assert.JSONEq(t, "{\"exists\":true,\"content\":\"created\\nupdated\",\"files\":[\"hello.txt\"]}", string(data))
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

func Test_GoJa_KVUsesUserDBWhenConfigured(t *testing.T) {
	db, _ := kv.NewMemory("")
	userDB, _ := kv.NewMemory("")
	server := testcore.NewTestServerWithKVOptions("example.com", db, userDB)
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function() {
  const store = kv.repo("group")
  store.set("key", "value")
  return new Response(store.get("key"))
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "api/**"
  js:
    exec: "index.js"
`)

	data, _, err := server.OpenFile("https://org1.example.com/repo1/api/test")
	assert.NoError(t, err)
	assert.Equal(t, "value", string(data))

	got, err := userDB.Child("repo", "org1", "repo1", "group").Get(context.Background(), "key")
	assert.NoError(t, err)
	assert.Equal(t, "value", got)

	_, err = db.Child("repo", "org1", "repo1", "group").Get(context.Background(), "key")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func Test_GoJa_KVFallsBackToDBWhenUserDBMissing(t *testing.T) {
	db, _ := kv.NewMemory("")
	server := testcore.NewTestServerWithKVOptions("example.com", db, nil)
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function() {
  const store = kv.repo("group")
  store.set("key", "value")
  return new Response(store.get("key"))
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "api/**"
  js:
    exec: "index.js"
`)

	data, _, err := server.OpenFile("https://org1.example.com/repo1/api/test")
	assert.NoError(t, err)
	assert.Equal(t, "value", string(data))

	got, err := db.Child("repo", "org1", "repo1", "group").Get(context.Background(), "key")
	assert.NoError(t, err)
	assert.Equal(t, "value", got)
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

func Test_GoJa_FetchDefaultResponseBodyLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("b", (4<<20)+1)))
	}))
	defer ts.Close()

	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function() {
  const res = await fetch('%s')
  return new Response(await res.text())
})
`, ts.URL)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "**"
  js:
    exec: "index.js"
`)

	httpServer := server.StartHTTPServer("org1.example.com")
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/repo1/fetch")
	if !assert.NoError(t, err) {
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.True(t,
		strings.Contains(string(body), "fetch response body exceeds limit") ||
			strings.Contains(string(body), "context canceled"),
	)
}

func Test_GoJa_FetchRequestObject(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Method", r.Method)
		w.Header().Set("X-Value", r.Header.Get("X-Test"))
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function() {
  const req = new Request('%s', {
    method: 'post',
    headers: [['X-Test', '123']],
  })
  const res = await fetch(req)
  return Response.json({
    body: await res.text(),
    method: res.headers.get('X-Method'),
    value: res.headers.get('X-Value'),
    statusText: res.statusText,
  })
})
`, ts.URL)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "**"
  js:
    exec: "index.js"
`)

	data, _, err := server.OpenFile("https://org1.example.com/repo1/fetch")
	assert.NoError(t, err)
	assert.JSONEq(t, `{"body":"ok","method":"POST","value":"123","statusText":"OK"}`, string(data))
}

func Test_GoJa_FetchResponseStream(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("streamed"))
	}))
	defer ts.Close()

	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function() {
  const res = await fetch('%s')
  const reader = res.body.getReader()
  const first = await reader.read({ size: 3 })
  const second = await reader.read({ size: 16 })
  const done = await reader.read()
  return Response.json({
    first: new TextDecoder().decode(first.value),
    second: new TextDecoder().decode(second.value),
    done: done.done,
  })
})
`, ts.URL)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "**"
  js:
    exec: "index.js"
`)

	data, _, err := server.OpenFile("https://org1.example.com/repo1/fetch-stream")
	assert.NoError(t, err)
	assert.JSONEq(t, `{"first":"str","second":"eamed","done":true}`, string(data))
}

func Test_GoJa_AbortSignalBehavior(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(200 * time.Millisecond):
			_, _ = w.Write([]byte("late"))
		}
	}))
	defer ts.Close()

	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function() {
  const controller = new AbortController()
  setTimeout(() => controller.abort(), 20)
  const abortErr = await fetch('%s', { signal: controller.signal })
    .then(() => "resolved")
    .catch(err => String(err))
  const fetchErr = await fetch("https://example.com", {
    signal: { aborted: false },
  }).then(() => "resolved").catch(err => String(err))
  let requestErr = "missing-error"
  try {
    new Request("https://example.com", {
      signal: { aborted: false },
    })
  } catch (err) {
    requestErr = String(err)
  }
  return Response.json({ abortErr, fetchErr, requestErr })
})
`, ts.URL)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "**"
  js:
    exec: "index.js"
`)

	data, _, err := server.OpenFile("https://org1.example.com/repo1/abort-signal")
	assert.NoError(t, err)
	assert.JSONEq(t, `{"abortErr":"fetch aborted","fetchErr":"invalid abort signal","requestErr":"invalid abort signal"}`, string(data))
}

func Test_GoJa_ResponseErrorsAreCatchable(t *testing.T) {
	server := newGoJaTestServer(`
serve(async function() {
  let constructorErr = "missing-error"
  try {
    new Response({ value: 1 })
  } catch (err) {
    constructorErr = String(err)
  }
  let jsonErr = "missing-error"
  try {
    Response.json(function nope() {})
  } catch (err) {
    jsonErr = String(err)
  }
  return Response.json({ constructorErr, jsonErr })
})
`)
	defer server.Close()

	data, _, err := server.OpenFile("https://org1.example.com/repo1/response-errors")
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"constructorErr":"unsupported body type:`)
	assert.Contains(t, string(data), `"jsonErr":"json: unsupported type:`)
}

func Test_GoJa_BodyUsed(t *testing.T) {
	server := newGoJaTestServer(`
serve(async function(request) {
  await request.text()
  try {
    await request.text()
  } catch (err) {
    return new Response(String(err))
  }
  return new Response("missing-error", { status: 500 })
})
`)
	defer server.Close()

	data, _, err := server.OpenRequest(http.MethodPost, "https://org1.example.com/repo1/body", bytes.NewBufferString("payload"))
	assert.NoError(t, err)
	assert.Contains(t, string(data), "body stream already read")
}

func Test_GoJa_HeadersArrayInit(t *testing.T) {
	server := newGoJaTestServer(`
serve(async function() {
  const headers = new Headers([
    ['X-Test', '1'],
    ['X-Test-2', '2'],
  ])
  return new Response(headers.get('X-Test') + ':' + headers.get('X-Test-2'))
})
`)
	defer server.Close()

	data, _, err := server.OpenFile("https://org1.example.com/repo1/headers")
	assert.NoError(t, err)
	assert.Equal(t, "1:2", string(data))
}

func Test_GoJa_BodyStreamAndBytes(t *testing.T) {
	server := newGoJaTestServer(`
serve(async function(request) {
  const reader = request.body.getReader()
  const first = await reader.read()
  const second = await reader.read()
  return Response.json({
    first: new TextDecoder().decode(first.value),
    done: second.done,
    bytes: new TextDecoder().decode(await new Response("abc").bytes()),
  })
})
`)
	defer server.Close()

	data, _, err := server.OpenRequest(http.MethodPost, "https://org1.example.com/repo1/body-stream", bytes.NewBufferString("payload"))
	assert.NoError(t, err)
	assert.JSONEq(t, `{"first":"payload","done":true,"bytes":"abc"}`, string(data))
}

func Test_GoJa_ResponseStream(t *testing.T) {
	server := newGoJaTestServer(`
serve(async function() {
  const { response, stream } = http.stream({
    headers: { "Content-Type": "text/plain; charset=utf-8" }
  })
  void (async () => {
    await stream.write("hel")
    await stream.write("lo")
    await stream.flush()
    await stream.close()
  })()
  return response
})
`)
	defer server.Close()

	data, resp, err := server.OpenFile("https://org1.example.com/repo1/stream")
	assert.NoError(t, err)
	assert.Equal(t, "text/plain; charset=utf-8", resp.Header.Get("Content-Type"))
	assert.Equal(t, "hello", string(data))
}

func Test_GoJa_ResponseStreamCloseBeforeWrite(t *testing.T) {
	server := newGoJaTestServer(`
serve(async function() {
  const { response, stream } = http.stream()
  await stream.close()
  return response
})
`)
	defer server.Close()

	data, resp, err := server.OpenFile("https://org1.example.com/repo1/stream-close")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, string(data))
}

func Test_GoJa_BlobAndFormData(t *testing.T) {
	server := newGoJaTestServer(`
serve(async function(request) {
  const blob = await new Response("hello", {
    headers: { "Content-Type": "text/plain" }
  }).blob()
  const form = await request.formData()
  return Response.json({
    blobText: await blob.text(),
    blobType: blob.type,
    a: form.get("a"),
    b: form.get("b"),
  })
})
`)
	defer server.Close()

	body := bytes.NewBufferString("a=1&b=two")
	data, _, err := server.OpenRequest(http.MethodPost, "https://org1.example.com/repo1/form", body)
	assert.NoError(t, err)
	assert.JSONEq(t, `{"blobText":"hello","blobType":"text/plain","a":"1","b":"two"}`, string(data))
}

func Test_GoJa_NullishInputsDoNotCrash(t *testing.T) {
	server := newGoJaTestServer(`
serve(async function() {
  const headers = new Headers(undefined)
  headers.set("x-one", "1")

  const req = new Request("https://example.com", undefined)
  const req2 = new Request("https://example.com", {
    headers: null,
    body: null,
    signal: null,
  })

  const resp = new Response(null, undefined)
  const decoder = new TextDecoder()

  return Response.json({
    header: headers.get("x-one"),
    reqMethod: req.method,
    req2Method: req2.method,
    req2BodyUsed: req2.bodyUsed,
    respStatus: resp.status,
    decodeNull: decoder.decode(null),
    decodeUndefined: decoder.decode(undefined),
  })
})
`)
	defer server.Close()

	data, _, err := server.OpenFile("https://org1.example.com/repo1/nullish")
	assert.NoError(t, err)
	assert.JSONEq(t, `{"header":"1","reqMethod":"GET","req2Method":"GET","req2BodyUsed":false,"respStatus":200,"decodeNull":"","decodeUndefined":""}`, string(data))
}

func Test_GoJa_NullishHandlerRejectionIsGraceful(t *testing.T) {
	server := newGoJaTestServer(`
serve(async function() {
  return Promise.reject(undefined)
})
`)
	defer server.Close()

	_, resp, err := server.OpenFile("https://org1.example.com/repo1/reject")
	assert.Error(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
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

func Test_GoJa_FrameworkReadAndHeaders(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
const app = http.router()

app.post("/repo1/api/v1/json", async (request) => {
  const body = await http.read(request, "json")
  return await http.withHeaders(http.json(body), {
    "x-mode": "json",
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
	assert.Equal(t, "json", resp.Header.Get("X-Mode"))
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
	if !assert.NoError(t, err) {
		return
	}
	defer conn.Close()

	assert.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("hello")))
	messageType, payload, err := conn.ReadMessage()
	assert.NoError(t, err)
	assert.Equal(t, websocket.TextMessage, messageType)
	assert.Equal(t, "ECHO: hello", string(payload))
}

func Test_GoJa_EventAndVersionEventAreIsolated(t *testing.T) {
	server := newGoJaTestServer(`
serve(async function() {
  const sharedLoad = event.load("topic")
  const versionLoad = versionEvent.load("topic")
  await event.put("topic", "shared")
  await versionEvent.put("topic", "version")
  return Response.json({
    shared: await sharedLoad,
    version: await versionLoad,
  })
})
`)
	defer server.Close()

	data, _, err := server.OpenFile("https://org1.example.com/repo1/isolation")
	assert.NoError(t, err)
	assert.JSONEq(t, `{"shared":"shared","version":"version"}`, string(data))
}

func Test_GoJa_WebSocketSharedEventBroadcast(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(function(request) {
  const name = new URL(request.url).searchParams.get("name")?.trim()
  if (!name) throw new Error("Missing or empty name parameter")

  const { socket, response } = upgradeWebSocket(request)

  const pump = async () => {
    while (true) {
      await socket.send(await event.load("messages"))
    }
  }

  socket.addEventListener("message", async (evt) => {
    const data = typeof evt.data === "string" ? evt.data : ""
    if (data.trim()) {
      await event.put("messages", JSON.stringify({ name, data: data.trim() }))
    }
  })

  void pump()
  return response
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "event"
  js:
    exec: "index.js"
`)

	httpServer := server.StartHTTPServer("org1.example.com")
	defer httpServer.Close()

	baseURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/repo1/event"
	connA, _, err := websocket.DefaultDialer.Dial(baseURL+"?name=a", nil)
	if !assert.NoError(t, err) {
		return
	}
	defer connA.Close()

	connB, _, err := websocket.DefaultDialer.Dial(baseURL+"?name=b", nil)
	if !assert.NoError(t, err) {
		return
	}
	defer connB.Close()

	assert.NoError(t, connA.WriteMessage(websocket.TextMessage, []byte("hello")))

	type result struct {
		messageType int
		payload     string
		err         error
	}
	read := func(conn *websocket.Conn) <-chan result {
		ch := make(chan result, 1)
		go func() {
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			messageType, payload, err := conn.ReadMessage()
			ch <- result{messageType: messageType, payload: string(payload), err: err}
		}()
		return ch
	}

	results := []<-chan result{read(connA), read(connB)}
	for _, ch := range results {
		got := <-ch
		assert.NoError(t, got.err)
		if got.err == nil {
			assert.Equal(t, websocket.TextMessage, got.messageType)
			assert.JSONEq(t, `{"name":"a","data":"hello"}`, got.payload)
		}
	}
}

func Test_GoJa_EventOverflowRejectsPendingStream(t *testing.T) {
	server := newGoJaTestServerWithFilterConfig(`
serve(async function() {
  const first = event.load("topic")
  await event.put("topic", "one")
  await event.put("topic", "two")
  await event.put("topic", "three")
  await new Promise(resolve => setTimeout(resolve, 10))
  const second = await event.load("topic")

  let overflow = "missing"
  try {
    await event.load("topic")
  } catch (err) {
    overflow = String(err)
  }

  const afterOverflow = event.load("topic")
  await event.put("topic", "four")

  return Response.json({
    first: await first,
    second,
    overflow,
    afterOverflow: await afterOverflow,
  })
})
`, map[string]map[string]any{
		"js": {
			"realtime": map[string]any{
				"event_buffer": 1,
			},
		},
	})
	defer server.Close()

	data, _, err := server.OpenFile("https://org1.example.com/repo1/overflow")
	assert.NoError(t, err)
	assert.JSONEq(t, `{"first":"one","second":"two","overflow":"event backlog overflow","afterOverflow":"four"}`, string(data))
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
	if !assert.NoError(t, err) {
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

func Test_GoJa_RealtimeIORejectsAfterCommit(t *testing.T) {
	cases := []struct {
		name      string
		routePath string
		script    string
		ws        bool
		assertion func(*testing.T, string)
	}{
		{
			name:      "websocket rejects sse",
			routePath: "ws-owns",
			ws:        true,
			script: `
serve(function(request) {
  const ws = upgradeWebSocket(request)
  const sse = http.sse()

  ws.socket.addEventListener("open", async () => {
    try {
      await sse.stream.send("bad")
      await ws.socket.send("unexpected success")
    } catch (err) {
      await ws.socket.send(String(err))
    }
    ws.socket.close()
  })

  return ws.response
})
`,
			assertion: func(t *testing.T, body string) {
				assert.Equal(t, "event stream is unavailable: response already committed", body)
			},
		},
		{
			name:      "sse rejects websocket",
			routePath: "sse-owns",
			script: `
serve(function(request) {
  const ws = upgradeWebSocket(request)
  const sse = http.sse()

  ;(async () => {
    await sse.stream.send("ready", { event: "message", id: "1" })
    try {
      await ws.socket.send("bad")
      await sse.stream.send("unexpected success", { event: "error", id: "2" })
    } catch (err) {
      await sse.stream.send(String(err), { event: "error", id: "2" })
    }
    sse.stream.close()
  })()

  return sse.response
})
`,
			assertion: func(t *testing.T, body string) {
				assert.Contains(t, body, "data: ready\n")
				assert.Contains(t, body, "event: error\n")
				assert.Contains(t, body, "data: websocket is unavailable: response already committed\n")
			},
		},
		{
			name:      "response stream rejects websocket",
			routePath: "stream-owns",
			script: `
serve(function(request) {
  const out = http.stream()
  const ws = upgradeWebSocket(request)

  ;(async () => {
    await new Promise(resolve => setTimeout(resolve, 20))
    try {
      await ws.socket.send("bad")
      await out.stream.write("unexpected success")
    } catch (err) {
      await out.stream.write(String(err))
    }
    await out.stream.close()
  })()

  return out.response
})
`,
			assertion: func(t *testing.T, body string) {
				assert.Equal(t, "websocket is unavailable: response already committed", body)
			},
		},
	}

	client := &http.Client{Timeout: 2 * time.Second}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newGoJaTestServer(tc.script, tc.routePath)
			defer server.Close()

			httpServer := server.StartHTTPServer("org1.example.com")
			defer httpServer.Close()

			var body string
			if tc.ws {
				wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/repo1/" + tc.routePath
				conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
				if !assert.NoError(t, err) {
					return
				}
				defer conn.Close()

				_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
				messageType, payload, err := conn.ReadMessage()
				assert.NoError(t, err)
				assert.Equal(t, websocket.TextMessage, messageType)
				body = string(payload)
			} else {
				resp, err := client.Get(httpServer.URL + "/repo1/" + tc.routePath)
				if !assert.NoError(t, err) {
					return
				}
				defer resp.Body.Close()

				payload, err := io.ReadAll(resp.Body)
				assert.NoError(t, err)
				body = string(payload)
			}

			tc.assertion(t, body)
		})
	}
}
