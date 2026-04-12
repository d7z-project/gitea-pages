package core

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.d7z.net/middleware/kv"
	"gopkg.d7z.net/middleware/tools"
)

type AuthService struct {
	provider AuthProvider
	config   AuthServiceConfig

	sessions *tools.KVCache[AuthSession]
	states   *tools.KVCache[AuthState]
	authz    *tools.KVCache[bool]
}

func NewAuthService(provider AuthProvider, store kv.KV, config AuthServiceConfig) *AuthService {
	if config.CookieName == "" {
		config.CookieName = "gitea_pages_session"
	}
	if config.CookieSameSite == 0 {
		config.CookieSameSite = http.SameSiteLaxMode
	}
	if config.SessionTTL == 0 {
		config.SessionTTL = 24 * time.Hour
	}
	if config.StateTTL == 0 {
		config.StateTTL = 5 * time.Minute
	}
	if config.AuthzCacheTTL == 0 {
		config.AuthzCacheTTL = 30 * time.Second
	}
	if config.OnUnauthorized == nil {
		config.OnUnauthorized = func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		}
	}
	if config.OnForbidden == nil {
		config.OnForbidden = func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		}
	}
	if config.OnMethodDenied == nil {
		config.OnMethodDenied = func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		}
	}
	return &AuthService{
		provider: provider,
		config:   config,
		sessions: tools.NewCache[AuthSession](store, "session", config.SessionTTL),
		states:   tools.NewCache[AuthState](store, "state", config.StateTTL),
		authz:    tools.NewCache[bool](store, "authz", config.AuthzCacheTTL),
	}
}

func (s *AuthService) Handle(w http.ResponseWriter, req *http.Request) error {
	switch req.URL.Path {
	case AuthPathLogin:
		return s.handleLogin(w, req)
	case AuthPathCallback:
		return s.handleCallback(w, req)
	case AuthPathLogout:
		return s.handleLogout(w, req)
	default:
		http.NotFound(w, req)
		return nil
	}
}

func (s *AuthService) RequireRepoAccess(w http.ResponseWriter, req *http.Request, page *PageContent) (bool, error) {
	if !page.Private {
		return true, nil
	}

	hadCookie := s.hasSessionCookie(req)
	sess, ok, err := s.loadSession(req.Context(), req)
	if err != nil {
		return false, err
	}
	if !ok {
		if hadCookie {
			s.clearSessionCookie(w)
		}
		http.Redirect(w, req, AuthPathLogin+"?return_to="+url.QueryEscape(req.URL.RequestURI()), http.StatusFound)
		return false, nil
	}
	*req = *req.WithContext(ContextWithAuthSession(req.Context(), sess))

	authorized, ok, err := s.loadAuthz(req.Context(), sess, page.Owner, page.Repo)
	if err != nil {
		return false, err
	}
	if !ok {
		authorized, err = s.provider.AuthorizeRepo(req.Context(), sess, page.Owner, page.Repo)
		if err != nil {
			return false, err
		}
		_ = s.storeAuthz(req.Context(), sess, page.Owner, page.Repo, authorized)
	}
	if !authorized {
		s.config.OnForbidden(w, req, errors.New(http.StatusText(http.StatusForbidden)))
		return false, nil
	}
	return true, nil
}

func (s *AuthService) AttachAuth(req *http.Request) error {
	sess, ok, err := s.loadSession(req.Context(), req)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	*req = *req.WithContext(ContextWithAuthSession(req.Context(), sess))
	return nil
}

func (s *AuthService) handleLogin(w http.ResponseWriter, req *http.Request) error {
	returnTo := sanitizeReturnTo(req.URL.Query().Get("return_to"))
	stateID := uuid.NewString()
	if err := s.states.Store(req.Context(), stateID, AuthState{
		ReturnTo: returnTo,
		ExpireAt: time.Now().Add(s.config.StateTTL),
	}); err != nil {
		return err
	}
	loginURL, err := s.provider.LoginURL(req.Context(), stateID)
	if err != nil {
		return err
	}
	http.Redirect(w, req, loginURL, http.StatusFound)
	return nil
}

func (s *AuthService) handleCallback(w http.ResponseWriter, req *http.Request) error {
	stateID := req.URL.Query().Get("state")
	if stateID == "" {
		s.config.OnUnauthorized(w, req, errors.New("missing auth state"))
		return nil
	}
	state, ok, err := s.states.Load(req.Context(), stateID)
	if err != nil {
		return err
	}
	if !ok || time.Now().After(state.ExpireAt) {
		s.config.OnUnauthorized(w, req, errors.New("invalid or expired auth state"))
		return nil
	}
	_ = s.states.Delete(req.Context(), stateID)

	sess, err := s.provider.HandleCallback(req.Context(), req)
	if err != nil {
		s.config.OnUnauthorized(w, req, err)
		return nil
	}
	if sess.ID == "" {
		sess.ID = uuid.NewString()
	}
	if sess.ExpireAt.IsZero() {
		sess.ExpireAt = time.Now().Add(s.config.SessionTTL)
	}
	if err = s.sessions.Store(req.Context(), sess.ID, *sess); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.config.CookieName,
		Value:    sess.ID,
		Path:     "/",
		Domain:   s.config.CookieDomain,
		HttpOnly: true,
		Secure:   s.config.CookieSecure,
		SameSite: s.config.CookieSameSite,
		Expires:  sess.ExpireAt,
	})
	http.Redirect(w, req, state.ReturnTo, http.StatusFound)
	return nil
}

func (s *AuthService) handleLogout(w http.ResponseWriter, req *http.Request) error {
	if req.Method != http.MethodPost {
		s.config.OnMethodDenied(w, req, errors.New(http.StatusText(http.StatusMethodNotAllowed)))
		return nil
	}
	sess, ok, err := s.loadSession(req.Context(), req)
	if err != nil {
		return err
	}
	if ok {
		_ = s.provider.Logout(req.Context(), sess)
		_ = s.sessions.Delete(req.Context(), sess.ID)
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *AuthService) loadSession(ctx context.Context, req *http.Request) (*AuthSession, bool, error) {
	cookie, err := req.Cookie(s.config.CookieName)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return nil, false, nil
		}
		return nil, false, err
	}
	sess, ok, err := s.sessions.Load(ctx, cookie.Value)
	if err != nil {
		return nil, false, err
	}
	if !ok || time.Now().After(sess.ExpireAt) {
		return nil, false, nil
	}
	return &sess, true, nil
}

func (s *AuthService) authzKey(sess *AuthSession, owner, repo string) string {
	return sess.Identity.Subject + "/" + owner + "/" + repo
}

func (s *AuthService) loadAuthz(ctx context.Context, sess *AuthSession, owner, repo string) (bool, bool, error) {
	return s.authz.Load(ctx, s.authzKey(sess, owner, repo))
}

func (s *AuthService) storeAuthz(ctx context.Context, sess *AuthSession, owner, repo string, allowed bool) error {
	return s.authz.Store(ctx, s.authzKey(sess, owner, repo), allowed)
}

func (s *AuthService) hasSessionCookie(req *http.Request) bool {
	_, err := req.Cookie(s.config.CookieName)
	return err == nil
}

func (s *AuthService) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.config.CookieName,
		Value:    "",
		Path:     "/",
		Domain:   s.config.CookieDomain,
		HttpOnly: true,
		Secure:   s.config.CookieSecure,
		SameSite: s.config.CookieSameSite,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func sanitizeReturnTo(returnTo string) string {
	if returnTo == "" {
		return "/"
	}
	if !strings.HasPrefix(returnTo, "/") || strings.HasPrefix(returnTo, "//") {
		return "/"
	}
	return returnTo
}
