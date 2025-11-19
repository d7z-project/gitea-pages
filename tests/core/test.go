package core

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gopkg.d7z.net/gitea-pages/pkg"
	"gopkg.d7z.net/middleware/cache"
	"gopkg.d7z.net/middleware/kv"
	"gopkg.d7z.net/middleware/subscribe"
)

type TestServer struct {
	server *pkg.Server
	dummy  *ProviderDummy
}

func NewDefaultTestServer() *TestServer {
	return NewTestServer("example.com")
}

func NewTestServer(domain string) *TestServer {
	atom := zap.NewAtomicLevel()
	getenv := os.Getenv("BM")
	if getenv != "" {
		atom.SetLevel(zap.ErrorLevel)
	} else {
		atom.SetLevel(zap.DebugLevel)
	}
	cfg := zap.NewProductionConfig()
	cfg.Level = atom
	logger, _ := cfg.Build()
	zap.ReplaceGlobals(logger)
	dummy, err := NewDummy()
	if err != nil {
		zap.S().Fatal(err)
	}
	memoryCache, _ := cache.NewMemoryCache(cache.MemoryCacheConfig{
		MaxCapacity: 256,
		CleanupInt:  time.Minute,
	})
	memoryKV, _ := kv.NewMemory("")
	server, err := pkg.NewPageServer(
		http.DefaultClient,
		dummy,
		domain,
		"gh-pages",
		memoryKV,
		subscribe.NewMemorySubscriber(),
		memoryKV.Child("cache"),
		0,
		memoryCache,
		0,
		func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, os.ErrNotExist) {
				http.Error(w, "page not found.", http.StatusNotFound)
			} else if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		},
		make(map[string]map[string]any),
	)
	if err != nil {
		panic(err)
	}
	return &TestServer{
		dummy:  dummy,
		server: server,
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
	recorder := httptest.NewRecorder()
	t.server.ServeHTTP(recorder, httptest.NewRequest(method, url, body))
	response := recorder.Result()
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
