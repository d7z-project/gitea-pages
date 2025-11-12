package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/gitea-pages/tests/core"
)

func Test_filter_failback(t *testing.T) {
	server := core.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/404.html", "404 page")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "**"
  failback:
    path: index.html
  
`)
	//data, _, err := server.OpenFile("https://org1.example.com/repo1/")
	//assert.NoError(t, err)
	//assert.Equal(t, "hello world", string(data))
	//
	//// 测试默认回退
	//data, _, err = server.OpenFile("https://org1.example.com/repo1/404")
	//assert.NoError(t, err)
	//assert.Equal(t, "hello world", string(data))

	// 测试存在的页面
	data, _, err := server.OpenFile("https://org1.example.com/repo1/404.html")
	assert.NoError(t, err)
	assert.Equal(t, "404 page", string(data))
}
