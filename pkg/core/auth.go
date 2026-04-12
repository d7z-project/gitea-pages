package core

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type authContextKey struct{}

const (
	AuthPathLogin    = "/.pages/auth/login"
	AuthPathCallback = "/.pages/auth/callback"
	AuthPathLogout   = "/.pages/auth/logout"
)

type AuthProvider interface {
	LoginURL(ctx context.Context, state string) (string, error)
	HandleCallback(ctx context.Context, req *http.Request) (*AuthSession, error)
	AuthorizeRepo(ctx context.Context, sess *AuthSession, owner, repo string) (bool, error)
	Logout(ctx context.Context, sess *AuthSession) error
}

type AuthIdentity struct {
	Subject string `json:"subject"`
	Name    string `json:"name"`
}

type AuthSession struct {
	ID       string          `json:"id"`
	Identity AuthIdentity    `json:"identity"`
	Private  json.RawMessage `json:"private"`
	ExpireAt time.Time       `json:"expire_at"`
}

type AuthInfo struct {
	Authenticated bool          `json:"authenticated"`
	Identity      *AuthIdentity `json:"identity,omitempty"`
}

type AuthState struct {
	ReturnTo string    `json:"return_to"`
	ExpireAt time.Time `json:"expire_at"`
}

type AuthServiceConfig struct {
	CookieName     string
	CookieDomain   string
	CookieSecure   bool
	CookieSameSite http.SameSite
	SessionTTL     time.Duration
	StateTTL       time.Duration
	AuthzCacheTTL  time.Duration
	OnUnauthorized func(w http.ResponseWriter, r *http.Request, err error)
	OnForbidden    func(w http.ResponseWriter, r *http.Request, err error)
	OnMethodDenied func(w http.ResponseWriter, r *http.Request, err error)
}

func IsReservedPath(path string) bool {
	return path == AuthPathLogin || path == AuthPathCallback || path == AuthPathLogout || strings.HasPrefix(path, "/.pages/")
}

func ContextWithAuthSession(ctx context.Context, sess *AuthSession) context.Context {
	if sess == nil {
		return ctx
	}
	return context.WithValue(ctx, authContextKey{}, sess)
}

func AuthSessionFromContext(ctx context.Context) (*AuthSession, bool) {
	if ctx == nil {
		return nil, false
	}
	value, ok := ctx.Value(authContextKey{}).(*AuthSession)
	if !ok || value == nil {
		return nil, false
	}
	return value, true
}

func AuthInfoFromContext(ctx context.Context) AuthInfo {
	session, ok := AuthSessionFromContext(ctx)
	if !ok {
		return AuthInfo{Authenticated: false}
	}
	identity := session.Identity
	return AuthInfo{
		Authenticated: true,
		Identity:      &identity,
	}
}
