package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/gitea-pages/tests/core"
)

func Test_get_simple_html(t *testing.T) {
	server := core.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	data, _, err := server.OpenFile("https://org1.example.com/repo1/")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	_, resp, _ := server.OpenFile("https://org1.example.com/repo1/404")
	assert.NotNil(t, resp)
	assert.Equal(t, 404, resp.StatusCode)
}

func Test_get_alias(t *testing.T) {
	server := core.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
alias:
  - www.example.org
`)
	_, resp, _ := server.OpenFile("https://www.example.org")
	assert.Equal(t, 404, resp.StatusCode)

	_, resp, _ = server.OpenFile("https://org1.example.com/repo1/")
	assert.Equal(t, 302, resp.StatusCode)
	assert.Equal(t, "https://www.example.org/", resp.Header.Get("Location"))
	data, _, err := server.OpenFile("https://www.example.org")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
alias:
  - zzz.example.top
`)
	_, resp, _ = server.OpenFile("https://www.example.org")
	assert.Equal(t, 302, resp.StatusCode)
	assert.Equal(t, "https://zzz.example.top/", resp.Header.Get("Location"))

	_, resp, _ = server.OpenFile("https://www.example.org")
	assert.Equal(t, 404, resp.StatusCode)

	_, resp, _ = server.OpenFile("https://org1.example.com/repo1/")
	assert.Equal(t, 302, resp.StatusCode)
	assert.Equal(t, "https://zzz.example.top/", resp.Header.Get("Location"))

	_, resp, _ = server.OpenFile("https://org1.example.com/repo1/get/some")
	assert.Equal(t, 302, resp.StatusCode)
	assert.Equal(t, "https://zzz.example.top/get/some", resp.Header.Get("Location"))
}

func Test_fail_back(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		server := core.NewDefaultTestServer()
		defer server.Close()
		server.AddFile("org1/org1.example.com/gh-pages/index.html", "hello world")
		data, _, err := server.OpenFile("https://org1.example.com/")
		assert.NoError(t, err)
		assert.Equal(t, "hello world", string(data))
	})
	t.Run("child_default", func(t *testing.T) {
		server := core.NewDefaultTestServer()
		defer server.Close()
		server.AddFile("org1/org1.example.com/gh-pages/index.html", "hello world 1")
		server.AddFile("org1/org1.example.com/gh-pages/child/index.html", "hello world 2")
		data, _, err := server.OpenFile("https://org1.example.com/child/")
		assert.NoError(t, err)
		assert.Equal(t, "hello world 2", string(data))
	})

	t.Run("child_exist", func(t *testing.T) {
		server := core.NewDefaultTestServer()
		defer server.Close()
		server.AddFile("org1/org1.example.com/gh-pages/index.html", "hello world 1")
		server.AddFile("org1/org1.example.com/gh-pages/child/index.html", "hello world 2")
		server.AddFile("org1/child/gh-pages/index.html", "hello world 3")
		data, _, err := server.OpenFile("https://org1.example.com/child/")
		assert.NoError(t, err)
		assert.Equal(t, "hello world 3", string(data))
	})

	t.Run("child_exist_failback", func(t *testing.T) {
		server := core.NewDefaultTestServer()
		defer server.Close()
		server.AddFile("org1/org1.example.com/gh-pages/index.html", "hello world 1")
		server.AddFile("org1/org1.example.com/gh-pages/child/index.html", "hello world 2")
		server.AddFile("org1/child/gh-pages/no.html", "hello world 3")
		data, _, err := server.OpenFile("https://org1.example.com/child/")
		assert.NoError(t, err)
		assert.Equal(t, "hello world 2", string(data))
	})
}
