package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func TestNewGiteaRejectsHTTPAuthByDefault(t *testing.T) {
	_, err := NewGitea(http.DefaultClient, GiteaConfig{
		Server:       "http://127.0.0.1:3000",
		ClientID:     "cid",
		ClientSecret: "secret",
		RedirectURL:  "http://127.0.0.1/callback",
	})
	assert.Error(t, err)
}

func TestNewGiteaAllowsHTTPAuthForTests(t *testing.T) {
	provider, err := NewGitea(http.DefaultClient, GiteaConfig{
		Server:            "http://127.0.0.1:3000",
		ClientID:          "cid",
		ClientSecret:      "secret",
		RedirectURL:       "http://127.0.0.1/callback",
		AllowInsecureHTTP: true,
	})
	require.NoError(t, err)
	assert.NotNil(t, provider)
}

func TestGiteaCallbackAndAuthorizeRepo(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "token-1"})
		case "/api/v1/user":
			assert.Equal(t, "token token-1", r.Header.Get("Authorization"))
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "login": "dragon"})
		case "/api/v1/repos/org/repo":
			assert.Equal(t, "token token-1", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "repo"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	provider, err := NewGitea(ts.Client(), GiteaConfig{
		Server:            ts.URL,
		ClientID:          "cid",
		ClientSecret:      "secret",
		RedirectURL:       ts.URL + "/callback",
		AllowInsecureHTTP: true,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "https://pages.example.com/.pages/auth/callback?code=abc&state=s1", nil)
	session, err := provider.HandleCallback(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "42", session.Identity.Subject)
	assert.Equal(t, "dragon", session.Identity.Name)

	allowed, err := provider.AuthorizeRepo(context.Background(), &core.AuthSession{
		ID:       "sess-1",
		Identity: session.Identity,
		Private:  session.Private,
		ExpireAt: time.Now().Add(time.Hour),
	}, "org", "repo")
	require.NoError(t, err)
	assert.True(t, allowed)
}
