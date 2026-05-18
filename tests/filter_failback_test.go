package tests

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	testcore "gopkg.d7z.net/gitea-pages/tests/core"
)

func Test_Filter_FailbackServesFallbackForMissingRoute(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "home")
	server.AddFile("org1/repo1/gh-pages/app/index.html", "app shell")
	server.AddFile("org1/repo1/gh-pages/app/existing.txt", "real file")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "app/**"
  failback:
    path: app/index.html
`)

	data, resp, err := server.OpenFile("https://org1.example.com/repo1/app/dashboard")
	assert.NoError(t, err)
	assert.Equal(t, "app shell", string(data))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "public, max-age=60", resp.Header.Get("Cache-Control"))
}

func Test_Filter_FailbackKeepsExistingFileResponse(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "home")
	server.AddFile("org1/repo1/gh-pages/app/index.html", "app shell")
	server.AddFile("org1/repo1/gh-pages/app/existing.txt", "real file")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "app/**"
  failback:
    path: app/index.html
`)

	data, _, err := server.OpenFile("https://org1.example.com/repo1/app/existing.txt")
	assert.NoError(t, err)
	assert.Equal(t, "real file", string(data))
}
