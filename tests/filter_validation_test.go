package tests

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/gitea-pages/pkg"
	testcore "gopkg.d7z.net/gitea-pages/tests/core"
)

func Test_PageConfigUnknownFilterReturnsClearError(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "**"
  unknown_filter: {}
`)

	_, resp, err := server.OpenRequest(http.MethodGet, "https://org1.example.com/repo1/", nil)
	assert.Error(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func Test_PageConfigDisabledFilterReturnsClearError(t *testing.T) {
	server := testcore.NewTestServerOptions("example.com", pkg.WithFilterConfig(map[string]map[string]any{
		"js": {"enabled": false},
	}))
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "**"
  js:
    exec: "index.js"
`)

	_, resp, err := server.OpenRequest(http.MethodGet, "https://org1.example.com/repo1/", nil)
	assert.Error(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func Test_PageConfigInvalidRouteDoesNotCrashServer(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "bad repo")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: ["/api/**"]
  js:
    exec: "index.js"
`)
	server.AddFile("org1/repo2/gh-pages/index.html", "good repo")

	_, resp, err := server.OpenRequest(http.MethodGet, "https://org1.example.com/repo1/", nil)
	assert.Error(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	data, _, err := server.OpenFile("https://org1.example.com/repo2/")
	assert.NoError(t, err)
	assert.Equal(t, "good repo", string(data))
}
