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
)

type TestServer struct {
	server *pkg.Server
	dummy  *ProviderDummy
}

type SvcOpts func(options *pkg.ServerOptions)

func NewDefaultTestServer() *TestServer {
	return NewTestServer("example.com", func(options *pkg.ServerOptions) {
		options.CacheMetaTTL = 0
	})
}

func NewTestServer(domain string, opts ...SvcOpts) *TestServer {
	atom := zap.NewAtomicLevel()
	atom.SetLevel(zap.DebugLevel)
	cfg := zap.NewProductionConfig()
	cfg.Level = atom
	logger, _ := cfg.Build()
	zap.ReplaceGlobals(logger)
	options := pkg.DefaultOptions(domain)
	for _, opt := range opts {
		opt(&options)
	}
	dummy, err := NewDummy()
	if err != nil {
		zap.S().Fatal(err)
	}

	server := pkg.NewPageServer(dummy, options)

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
	t.server.ServeHTTP(recorder, httptest.NewRequest("GET", url, nil))
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
	return t.server.Close()
}
