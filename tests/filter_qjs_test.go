package tests

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/gitea-pages/tests/core"
)

func Test_JS(t *testing.T) {
	server := core.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
function get(a,b) {
  return a + b;
}
response.write('512 + 512 = ' + get(512,512))
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "api/v1/**"
  qjs:
    exec: "index.js"
`)
	data, _, err := server.OpenFile("https://org1.example.com/repo1/")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	data, _, err = server.OpenFile("https://org1.example.com/repo1/api/v1/get")
	assert.NoError(t, err)
	assert.Equal(t, "512 + 512 = 1024", string(data))
}

func Test_JS_Request(t *testing.T) {
	server := core.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `response.write(request.method+' /'+request.path)`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "api/v1/**"
  qjs:
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
