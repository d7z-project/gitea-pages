package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/gitea-pages/tests/core"
)

func TestProxy(t *testing.T) {
	t.Skip()
	server := core.NewDefaultTestServer()
	hs := core.NewServer()
	defer server.Close()
	defer hs.Close()
	hs.Add("/test/data", "hello data")
	hs.Add("/test/", "hello proxy")

	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: /api/**
  reverse_proxy:
    prefix: /api
    target: %s
proxy:
  /abi: %s/
`, hs.URL, hs.URL)
	data, _, err := server.OpenFile("https://org1.example.com/repo1/")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	data, _, err = server.OpenFile("https://org1.example.com/repo1/api")
	assert.NoError(t, err)
	assert.Equal(t, "hello proxy", string(data))
	data, _, err = server.OpenFile("https://org1.example.com/repo1/api/data")
	assert.NoError(t, err)
	assert.Equal(t, "hello data", string(data))

	_, resp, _ := server.OpenFile("https://org1.example.com/repo1/abi/data")
	assert.Equal(t, 404, resp.StatusCode)
}

func TestCnameProxy(t *testing.T) {
	t.Skip()
	server := core.NewDefaultTestServer()
	hs := core.NewServer()
	defer server.Close()
	defer hs.Close()
	hs.Add("/test/", "hello proxy")

	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
alias:
  - www.example.org
proxy:
  /api: %s/test
`, hs.URL)
	_, resp, _ := server.OpenFile("https://org1.example.com/repo1/")
	assert.Equal(t, 302, resp.StatusCode)
	assert.Equal(t, "https://www.example.org/", resp.Header.Get("Location"))
	data, _, err := server.OpenFile("https://www.example.org")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	_, resp, _ = server.OpenFile("https://org1.example.com/repo1/api")
	assert.Equal(t, 302, resp.StatusCode)
	assert.Equal(t, "https://www.example.org/api", resp.Header.Get("Location"))

	data, _, err = server.OpenFile("https://www.example.org/api")
	assert.NoError(t, err)
	assert.Equal(t, "hello proxy", string(data))
}
