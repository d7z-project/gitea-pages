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
	hs.Add("/test/", "hello proxy")

	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
proxy:
  /api: %s/test
`, hs.URL)
	data, _, err := server.OpenFile("https://org1.example.com/repo1/")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	data, _, err = server.OpenFile("https://org1.example.com/repo1/api")
	assert.NoError(t, err)
	assert.Equal(t, "hello proxy", string(data))
}
