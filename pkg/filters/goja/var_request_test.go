package goja

import (
	"net/http"
	"net/http/httptest"
	"testing"

	js "github.com/dop251/goja"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func TestRequestInjectAuth(t *testing.T) {
	vm := js.New()
	req := httptest.NewRequest(http.MethodGet, "https://example.com/repo/api", nil)
	req = req.WithContext(core.ContextWithAuthSession(req.Context(), &core.AuthSession{
		ID: "sess-1",
		Identity: core.AuthIdentity{
			Subject: "u1",
			Name:    "dragon",
		},
	}))

	err := RequestInject(core.FilterContext{
		PageContent: &core.PageContent{Path: "api"},
	}, vm, req, RequestConfig{})
	require.NoError(t, err)

	value := vm.Get("request")
	obj := value.ToObject(vm)
	auth := obj.Get("auth").ToObject(vm)
	assert.True(t, auth.Get("authenticated").ToBoolean())

	identity := auth.Get("identity").ToObject(vm)
	assert.Equal(t, "u1", identity.Get("subject").String())
	assert.Equal(t, "dragon", identity.Get("name").String())
}
