package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/gitea-pages/tests/core"
)

func Test_filter_block(t *testing.T) {
	server := core.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/bad.html", "hello world")
	data, _, err := server.OpenFile("https://org1.example.com/repo1/")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	data, _, err = server.OpenFile("https://org1.example.com/repo1/bad.html")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "bad.html"
  block:
   code:
`)
	data, _, err = server.OpenFile("https://org1.example.com/repo1/")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))
	_, resp, _ := server.OpenFile("https://org1.example.com/repo1/bad.html")
	assert.Equal(t, 403, resp.StatusCode)
	// 默认排除的内容
	_, resp, _ = server.OpenFile("https://org1.example.com/repo1/.pages.yaml")
	assert.Equal(t, 403, resp.StatusCode)
}
