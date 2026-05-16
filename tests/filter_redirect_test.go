package tests

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	testcore "gopkg.d7z.net/gitea-pages/tests/core"
)

func Test_Filter_RedirectPreservesPathQueryAndScheme(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "home")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "legacy/**"
  redirect:
    targets:
      - target.example.com
    code: 301
`)

	_, resp, err := server.OpenFile("https://org1.example.com/repo1/legacy/docs/index.html?q=1")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusMovedPermanently, resp.StatusCode)
	assert.Equal(t, "https://target.example.com/legacy/docs/?q=1", resp.Header.Get("Location"))
}
