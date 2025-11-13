package core

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gopkg.d7z.net/gitea-pages/pkg"
	"gopkg.d7z.net/middleware/kv"
	"gopkg.d7z.net/middleware/tools"
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
	atom.SetLevel(zap.DebugLevel)
	cfg := zap.NewProductionConfig()
	cfg.Level = atom
	logger, _ := cfg.Build()
	zap.ReplaceGlobals(logger)
	dummy, err := NewDummy()
	if err != nil {
		zap.S().Fatal(err)
	}

	memoryKV, _ := kv.NewMemory("")
	server := pkg.NewPageServer(
		http.DefaultClient,
		dummy,
		domain,
		"gh-pages",
		memoryKV,
		tools.NewPrefixKV(memoryKV, "cache"),
		0,
		func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, os.ErrNotExist) {
				http.Error(w, "page not found.", http.StatusNotFound)
			} else if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		},
	)

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
	recorder := httptest.NewRecorder()
	t.server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, url, nil))
	response := recorder.Result()
	if response.Body != nil {
		defer response.Body.Close()
	}
	if response.StatusCode != http.StatusOK {
		return nil, response, errors.New(response.Status)
	}
	all, _ := io.ReadAll(response.Body)
	return all, response, nil
}

func (t *TestServer) Close() error {
	return nil
}
