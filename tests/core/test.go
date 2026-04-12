package core

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	"gopkg.d7z.net/gitea-pages/pkg"
	"gopkg.d7z.net/middleware/cache"
	"gopkg.d7z.net/middleware/kv"
	"gopkg.d7z.net/middleware/subscribe"
)

type TestServer struct {
	server  *pkg.Server
	dummy   *ProviderDummy
	cookies map[string]*http.Cookie
}

func NewDefaultTestServer() *TestServer {
	return NewTestServer("example.com")
}

func NewTestServer(domain string) *TestServer {
	return NewTestServerOptions(domain)
}

func NewTestServerOptions(domain string, opts ...pkg.ServerOption) *TestServer {
	memoryKV, _ := kv.NewMemory("")
	return NewTestServerWithKVOptions(domain, memoryKV, memoryKV, opts...)
}

func NewTestServerWithKVOptions(domain string, db, userDB kv.KV, opts ...pkg.ServerOption) *TestServer {
	level := slog.LevelDebug
	getenv := os.Getenv("BM")
	if getenv != "" {
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	dummy, err := NewDummy()
	if err != nil {
		panic(err)
	}
	memoryCache, _ := cache.NewMemoryCache(cache.MemoryCacheConfig{
		MaxCapacity: 256,
		CleanupInt:  time.Minute,
	})
	server, err := pkg.NewPageServer(
		dummy,
		domain,
		db,
		userDB,
		append([]pkg.ServerOption{
			pkg.WithClient(http.DefaultClient),
			pkg.WithEvent(subscribe.NewMemorySubscriber()),
			pkg.WithMetaCache(db.Child("cache"), 0, 0, 0),
			pkg.WithBlobCache(memoryCache, 0),
			pkg.WithErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
				if errors.Is(err, os.ErrNotExist) {
					http.Error(w, "page not found.", http.StatusNotFound)
				} else if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			}),
			pkg.WithFilterConfig(make(map[string]map[string]any)),
		}, opts...)...,
	)
	if err != nil {
		panic(err)
	}
	return &TestServer{
		dummy:   dummy,
		server:  server,
		cookies: map[string]*http.Cookie{},
	}
}

func (t *TestServer) AddFile(path, data string, args ...interface{}) {
	join := filepath.Join(t.dummy.BaseDir, path)
	err := os.MkdirAll(filepath.Dir(join), 0o755)
	if err != nil {
		panic(err)
	}
	err = os.WriteFile(join, []byte(fmt.Sprintf(data, args...)), 0o644)
	if err != nil {
		panic(err)
	}
}

func (t *TestServer) OpenFile(url string) ([]byte, *http.Response, error) {
	return t.OpenRequest(http.MethodGet, url, nil)
}

func (t *TestServer) OpenRequest(method, url string, body io.Reader) ([]byte, *http.Response, error) {
	return t.OpenRequestWithContext(context.Background(), method, url, body)
}

func (t *TestServer) OpenRequestWithContext(ctx context.Context, method, url string, body io.Reader) ([]byte, *http.Response, error) {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, url, body).WithContext(ctx)
	for _, cookie := range t.cookies {
		req.AddCookie(cookie)
	}
	t.server.ServeHTTP(recorder, req)
	response := recorder.Result()
	for _, cookie := range response.Cookies() {
		if cookie.MaxAge < 0 || cookie.Value == "" {
			delete(t.cookies, cookie.Name)
			continue
		}
		t.cookies[cookie.Name] = cookie
	}
	if response.Body != nil {
		defer response.Body.Close()
	}
	if response.StatusCode >= 400 {
		return nil, response, errors.New(response.Status)
	}
	all, _ := io.ReadAll(response.Body)
	return all, response, nil
}

func (t *TestServer) Close() error {
	return nil
}

func (t *TestServer) StartHTTPServer(host string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = host
		t.server.ServeHTTP(w, r)
	}))
}
