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
}
