package tests

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/gitea-pages/pkg"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	testcore "gopkg.d7z.net/gitea-pages/tests/core"
	"gopkg.d7z.net/middleware/kv"
)

type fakeAuthProvider struct {
	session    *core.AuthSession
	authorized bool
}

func (f *fakeAuthProvider) LoginURL(_ context.Context, state string) (string, error) {
	return "/provider-login?state=" + url.QueryEscape(state), nil
}

func (f *fakeAuthProvider) HandleCallback(context.Context, *http.Request) (*core.AuthSession, error) {
	return f.session, nil
}

func (f *fakeAuthProvider) AuthorizeRepo(context.Context, *core.AuthSession, string, string) (bool, error) {
	return f.authorized, nil
}

func (f *fakeAuthProvider) Logout(context.Context, *core.AuthSession) error {
	return nil
}

func newAuthTestServer(t *testing.T, provider core.AuthProvider) *testcore.TestServer {
	t.Helper()
	store, err := kv.NewMemory("")
	if err != nil {
		t.Fatal(err)
	}
	return testcore.NewTestServerOptions("example.com", pkg.WithAuth(core.NewAuthService(provider, store, core.AuthServiceConfig{
		CookieSecure: false,
		CookieName:   "test_session",
	})))
}

func Test_PrivateRepoRedirectsToLogin(t *testing.T) {
	server := newAuthTestServer(t, &fakeAuthProvider{})
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "private")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", "private: true\n")

	_, resp, err := server.OpenRequest(http.MethodGet, "https://org1.example.com/repo1/", nil)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, "/.pages/auth/login?return_to=%2Frepo1%2F", resp.Header.Get("Location"))
}

func Test_PrivateRepoCallbackCreatesSession(t *testing.T) {
	server := newAuthTestServer(t, &fakeAuthProvider{
		session: &core.AuthSession{
			ID:       "sess-1",
			Identity: core.AuthIdentity{Subject: "u1", Name: "dragon"},
			ExpireAt: time.Now().Add(time.Hour),
		},
		authorized: true,
	})
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "private")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", "private: true\n")

	_, resp, err := server.OpenRequest(http.MethodGet, "https://org1.example.com/.pages/auth/login?return_to=%2Frepo1%2F", nil)
	assert.NoError(t, err)
	loginLocation := resp.Header.Get("Location")
	assert.Contains(t, loginLocation, "/provider-login?state=")
	state := resp.Header.Get("Location")[len("/provider-login?state="):]

	_, resp, err = server.OpenRequest(http.MethodGet, "https://org1.example.com/.pages/auth/callback?state="+state+"&code=ok", nil)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, "/repo1/", resp.Header.Get("Location"))

	data, _, err := server.OpenFile("https://org1.example.com/repo1/")
	assert.NoError(t, err)
	assert.Equal(t, "private", string(data))
}

func Test_PrivateRepoDeniedReturnsForbidden(t *testing.T) {
	server := newAuthTestServer(t, &fakeAuthProvider{
		session: &core.AuthSession{
			ID:       "sess-1",
			Identity: core.AuthIdentity{Subject: "u1", Name: "dragon"},
			ExpireAt: time.Now().Add(time.Hour),
		},
		authorized: false,
	})
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "private")
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", "private: true\n")

	_, resp, err := server.OpenRequest(http.MethodGet, "https://org1.example.com/.pages/auth/login?return_to=%2Frepo1%2F", nil)
	assert.NoError(t, err)
	state := resp.Header.Get("Location")[len("/provider-login?state="):]
	_, _, _ = server.OpenRequest(http.MethodGet, "https://org1.example.com/.pages/auth/callback?state="+state+"&code=ok", nil)

	_, resp, err = server.OpenRequest(http.MethodGet, "https://org1.example.com/repo1/", nil)
	assert.Error(t, err)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func Test_InternalAuthPathOverridesPageContent(t *testing.T) {
	server := newAuthTestServer(t, &fakeAuthProvider{})
	defer server.Close()
	server.AddFile("org1/org1.example.com/gh-pages/.pages/auth/login", "shadowed")
	server.AddFile("org1/org1.example.com/gh-pages/index.html", "default")

	_, resp, err := server.OpenRequest(http.MethodGet, "https://org1.example.com/.pages/auth/login", nil)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Location"), "/provider-login?state=")
}
