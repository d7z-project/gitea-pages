package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/gitea-pages/tests/core"
)

func Test_Filter_Template(t *testing.T) {
	server := core.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/tmpl/index.html", "hello world,{{ .Request.Host }}")
	server.AddFile("org1/repo1/gh-pages/tmpl/ignore.html", "hello world, No Template")
	server.AddFile("org1/repo1/gh-pages/tmpl/include.txt", "master")
	server.AddFile("org1/repo1/gh-pages/tmpl/include.html", `hello world, {{ load "tmpl/include.txt" }}`)

	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: tmpl/index.html,tmpl/include.html
  template:
`)
	data, _, err := server.OpenFile("https://org1.example.com/repo1/")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	data, _, err = server.OpenFile("https://org1.example.com/repo1/tmpl/index.html")
	assert.NoError(t, err)
	assert.Equal(t, "hello world,org1.example.com", string(data))

	data, _, err = server.OpenFile("https://org1.example.com/repo1/tmpl/ignore.html")
	assert.NoError(t, err)
	assert.Equal(t, "hello world, No Template", string(data))
	data, _, err = server.OpenFile("https://org1.example.com/repo1/tmpl/include.html")
	assert.NoError(t, err)
	assert.Equal(t, "hello world, master", string(data))
}
