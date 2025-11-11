package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/gitea-pages/tests/core"

	_ "gopkg.d7z.net/gitea-pages/pkg/renders"
)

func Test_get_render(t *testing.T) {
	server := core.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/tmpl/index.html", "hello world,{{ .Request.Host }}")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
templates:
  gotemplate: tmpl/*.html
`)
	data, _, err := server.OpenFile("https://org1.example.com/repo1/")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	data, _, err = server.OpenFile("https://org1.example.com/repo1/tmpl/index.html")
	assert.NoError(t, err)
	assert.Equal(t, "hello world,org1.example.com", string(data))
}
