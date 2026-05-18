package tests

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/gitea-pages/pkg"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	testcore "gopkg.d7z.net/gitea-pages/tests/core"
)

func Test_Filter_DirectServesMatchedFile(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "home")
	server.AddFile("org1/repo1/gh-pages/assets/download/readme.txt", "download body")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "download/**"
  direct:
    prefix: assets
`)

	data, resp, err := server.OpenFile("https://org1.example.com/repo1/download/readme.txt")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "download body", string(data))
	assert.Equal(t, "public, max-age=60", resp.Header.Get("Cache-Control"))
}

func Test_Filter_DirectRedirectsDirectoryToTrailingSlash(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "home")
	server.AddFile("org1/repo1/gh-pages/assets/download/docs/index.html", "nested page")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "download/**"
  direct:
    prefix: assets
`)

	_, resp, err := server.OpenFile("https://org1.example.com/repo1/download/docs")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, "/repo1/download/docs/", resp.Header.Get("Location"))
	assert.Empty(t, resp.Header.Get("Cache-Control"))
}

func Test_Filter_DirectRejectsNonGetMethods(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "home")
	server.AddFile("org1/repo1/gh-pages/assets/download/readme.txt", "download body")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "download/**"
  direct:
    prefix: assets
`)

	_, resp, err := server.OpenRequest(http.MethodPost, "https://org1.example.com/repo1/download/readme.txt", bytes.NewBufferString("ignored"))
	assert.Error(t, err)
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func Test_Filter_DirectAllowsDisablingStaticCacheControl(t *testing.T) {
	server := testcore.NewTestServerOptions("example.com", pkg.WithFilterServerConfig(core.FilterServerConfig{}))
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "home")
	server.AddFile("org1/repo1/gh-pages/assets/download/readme.txt", "download body")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "download/**"
  direct:
    prefix: assets
`)

	data, resp, err := server.OpenFile("https://org1.example.com/repo1/download/readme.txt")
	assert.NoError(t, err)
	assert.Equal(t, "download body", string(data))
	assert.Empty(t, resp.Header.Get("Cache-Control"))
}
