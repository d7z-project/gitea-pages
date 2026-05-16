package tests

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	testcore "gopkg.d7z.net/gitea-pages/tests/core"
)

func Test_Filter_404ServesCustom404Page(t *testing.T) {
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "home")
	server.AddFile("org1/repo1/gh-pages/404.html", "custom not found")

	httpServer := server.StartHTTPServer("org1.example.com")
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/repo1/missing.txt")
	assert.NoError(t, err)
	if !assert.NotNil(t, resp) {
		return
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	assert.NoError(t, readErr)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, "custom not found", string(body))
}
