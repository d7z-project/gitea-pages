package tests

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/gitea-pages/tests/core"
)

func Test_GoJaJS(t *testing.T) {
	server := core.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
function get(a,b) {
  return a + b;
}
response.writeHead(201,{'X-Cache': 'ignore'});
console.log('hello world')
response.write('512 + 512 = ' + get(512,512))
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
	server := core.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `response.write(request.method+' /'+request.path)`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "api/v1/**"
  js:
    exec: "index.js"
`)
	data, _, err := server.OpenFile("https://org1.example.com/repo1/")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	data, _, err = server.OpenFile("https://org1.example.com/repo1/api/v1/fetch")
	assert.NoError(t, err)
	assert.Equal(t, "GET /api/v1/fetch", string(data))

	data, _, err = server.OpenRequest(http.MethodPost, "https://org1.example.com/repo1/api/v1/fetch", nil)
	assert.NoError(t, err)
	assert.Equal(t, "POST /api/v1/fetch", string(data))
}

func Test_GoJa_Async(t *testing.T) {
	server := core.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}
(async()=>{
    await sleep(1000)
    response.write('abc')
})()

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

func Benchmark_GoJa_Request(b *testing.B) {
	b.Setenv("BM", "1")
	server := core.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `response.write(request.method+' /'+request.path)`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "api/v1/**"
  js:
    exec: "index.js"
`)

	b.ResetTimer() // 重置计时器，只测量下面的操作

	b.Run("OpenFile_root", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			data, _, err := server.OpenFile("https://org1.example.com/repo1/")
			if err != nil {
				b.Fatal(err)
			}
			if string(data) != "hello world" {
				b.Fatalf("expected 'hello world', got '%s'", string(data))
			}
		}
	})

	b.Run("OpenFile_api", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			data, _, err := server.OpenFile("https://org1.example.com/repo1/api/v1/fetch")
			if err != nil {
				b.Fatal(err)
			}
			if string(data) != "GET /api/v1/fetch" {
				b.Fatalf("expected 'GET /api/v1/fetch', got '%s'", string(data))
			}
		}
	})

	b.Run("OpenRequest_post", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			data, _, err := server.OpenRequest(http.MethodPost, "https://org1.example.com/repo1/api/v1/fetch", nil)
			if err != nil {
				b.Fatal(err)
			}
			if string(data) != "POST /api/v1/fetch" {
				b.Fatalf("expected 'POST /api/v1/fetch', got '%s'", string(data))
			}
		}
	})
}
