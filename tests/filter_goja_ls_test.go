package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/gitea-pages/tests/core"
)

func Test_GoJa_LocalStorage(t *testing.T) {
	server := core.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
localStorage.setItem('foo', 'bar');
const val = localStorage.getItem('foo');
localStorage.removeItem('nonexistent');
response.write(val);
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "test"
  js:
    exec: "index.js"
`)

	data, _, err := server.OpenFile("https://org1.example.com/repo1/test")
	assert.NoError(t, err)
	assert.Equal(t, "bar", string(data))

	// 验证持久化
	server.AddFile("org1/repo1/gh-pages/index2.js", `
response.write(localStorage.getItem('foo'));
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "test"
  js:
    exec: "index.js"
- path: "check"
  js:
    exec: "index2.js"
`)
	data, _, err = server.OpenFile("https://org1.example.com/repo1/check")
	assert.NoError(t, err)
	assert.Equal(t, "bar", string(data))
}
