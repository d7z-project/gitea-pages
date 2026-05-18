package filters

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func TestRedirectUsesRequestInfoScheme(t *testing.T) {
	instance, err := FilterInstRedirect(core.GlobalFilterInit{})
	require.NoError(t, err)

	call, err := instance(core.Params{
		"targets": []string{"target.example.com"},
		"code":    http.StatusMovedPermanently,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://source.example.com/docs/index.html?q=1", nil)
	req.Host = "source.example.com"
	req = req.WithContext(core.ContextWithRequestInfo(req.Context(), core.RequestInfo{
		Scheme: "https",
		Host:   req.Host,
	}))

	rec := httptest.NewRecorder()
	nextCalled := false
	err = call(core.FilterContext{
		Context: req.Context(),
		PageContent: &core.PageContent{
			PageMetaContent: &core.PageMetaContent{
				Alias: []string{"target.example.com"},
			},
			Path: "docs/index.html",
		},
	}, rec, req, func(ctx core.FilterContext, writer http.ResponseWriter, request *http.Request) error {
		nextCalled = true
		return nil
	})

	require.NoError(t, err)
	assert.False(t, nextCalled)
	assert.Equal(t, http.StatusMovedPermanently, rec.Code)
	assert.Equal(t, "https://target.example.com/docs/?q=1", rec.Header().Get("Location"))
}
