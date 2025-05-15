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

	_, resp, err := server.OpenFile("https://org1.example.com/repo1/404")
	assert.NotNil(t, resp)
	assert.Equal(t, resp.StatusCode, 404)
}

func Test_get_alias(t *testing.T) {
	server := core.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
alias:
  - www.example.org
`)
	data, resp, err := server.OpenFile("https://www.example.org")
	assert.Equal(t, resp.StatusCode, 404)

	data, resp, err = server.OpenFile("https://org1.example.com/repo1/")
	assert.Equal(t, resp.StatusCode, 302)
	assert.Equal(t, resp.Header.Get("Location"), "https://www.example.org/")
	data, resp, err = server.OpenFile("https://www.example.org")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
alias:
  - zzz.example.top
`)
	data, resp, err = server.OpenFile("https://www.example.org")
	assert.Equal(t, resp.StatusCode, 302)
	assert.Equal(t, resp.Header.Get("Location"), "https://zzz.example.top/")

	data, resp, err = server.OpenFile("https://www.example.org")
	assert.Equal(t, resp.StatusCode, 404)

	data, resp, err = server.OpenFile("https://org1.example.com/repo1/")
	assert.Equal(t, resp.StatusCode, 302)
	assert.Equal(t, resp.Header.Get("Location"), "https://zzz.example.top/")

	data, resp, err = server.OpenFile("https://org1.example.com/repo1/get/some")
	assert.Equal(t, resp.StatusCode, 302)
	assert.Equal(t, resp.Header.Get("Location"), "https://zzz.example.top/get/some")
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

func Test_get_v_route(t *testing.T) {
	server := core.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
v-route: true
`)
	data, _, err := server.OpenFile("https://org1.example.com/repo1/")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	data, _, err = server.OpenFile("https://org1.example.com/repo1/404")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))
}

func Test_get_v_ignore(t *testing.T) {
	server := core.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/bad.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
ignore: .pages.yaml
`)
	data, _, err := server.OpenFile("https://org1.example.com/repo1/")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	data, _, err = server.OpenFile("https://org1.example.com/repo1/bad.html")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
ignore: bad.*
`)
	data, _, err = server.OpenFile("https://org1.example.com/repo1/")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))
	_, resp, _ := server.OpenFile("https://org1.example.com/repo1/bad.html")
	assert.Equal(t, 404, resp.StatusCode)
	// 默认排除的内容
	_, resp, _ = server.OpenFile("https://org1.example.com/repo1/.pages.yaml")
	assert.Equal(t, 404, resp.StatusCode)
}
