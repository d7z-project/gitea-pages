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
	assert.Equal(t, resp.Header.Get("Location"), "https://www.example.org/index.html")
	data, resp, err = server.OpenFile("https://www.example.org")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
alias:
  - zzz.example.top
`)
	data, resp, err = server.OpenFile("https://www.example.org")
	assert.Equal(t, resp.StatusCode, 302)
	assert.Equal(t, resp.Header.Get("Location"), "https://zzz.example.top/index.html")

	data, resp, err = server.OpenFile("https://www.example.org")
	assert.Equal(t, resp.StatusCode, 404)

	data, resp, err = server.OpenFile("https://org1.example.com/repo1/")
	assert.Equal(t, resp.StatusCode, 302)
	assert.Equal(t, resp.Header.Get("Location"), "https://zzz.example.top/index.html")
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
	_, resp, _ := server.OpenFile("https://org1.example.com/repo1/bad.html")
	assert.Equal(t, 404, resp.StatusCode)
}
