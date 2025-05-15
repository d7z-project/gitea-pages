package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/gitea-pages/tests/core"
)

func Test_proxy(t *testing.T) {
	server := core.NewDefaultTestServer()
	hs := core.NewServer()
	defer server.Close()
	defer hs.Close()
	hs.Add("/test/data", "hello data")
	hs.Add("/test/", "hello proxy")

	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
proxy:
  /api: %s/test
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

	_, resp, err := server.OpenFile("https://org1.example.com/repo1/abi/data")
	assert.Equal(t, resp.StatusCode, 404)
}

func Test_cname_proxy(t *testing.T) {
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
	_, resp, err := server.OpenFile("https://org1.example.com/repo1/")
	assert.Equal(t, resp.StatusCode, 302)
	assert.Equal(t, resp.Header.Get("Location"), "https://www.example.org/")
	data, resp, err := server.OpenFile("https://www.example.org")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	_, resp, err = server.OpenFile("https://org1.example.com/repo1/api")
	assert.Equal(t, resp.StatusCode, 302)
	assert.Equal(t, resp.Header.Get("Location"), "https://www.example.org/api")

	data, resp, err = server.OpenFile("https://www.example.org/api")
	assert.NoError(t, err)
	assert.Equal(t, "hello proxy", string(data))
}
