package pkg

import (
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"code.d7z.net/d7z-project/gitea-pages/pkg/core"
	"code.d7z.net/d7z-project/gitea-pages/pkg/utils"
	"github.com/pbnjay/memory"
	"github.com/pkg/errors"
)

type ServerOptions struct {
	Domain        string
	DefaultBranch string

	Config utils.Config
	Cache  utils.Cache

	MaxCacheSize int

	HttpClient *http.Client
}

func DefaultOptions(domain string) ServerOptions {
	configMemory, _ := utils.NewConfigMemory("")
	return ServerOptions{
		Domain:        domain,
		DefaultBranch: "gh-pages",
		Config:        configMemory,
		Cache:         utils.NewCacheMemory(1024*1024*10, int(memory.FreeMemory()/3*2)),
		MaxCacheSize:  1024 * 1024 * 10,
		HttpClient:    http.DefaultClient,
	}
}

type Server struct {
	meta    *core.PageDomain
	options *ServerOptions
	reader  *core.CacheBackendBlobReader
}

func NewPageServer(backend core.Backend, options ServerOptions) *Server {
	backend = core.NewCacheBackend(backend, options.Config, time.Minute)
	svcMeta := core.NewServerMeta(options.HttpClient, backend, options.Config, time.Minute)
	pageMeta := core.NewPageDomain(svcMeta, options.Config, options.Domain, options.DefaultBranch)
	reader := core.NewCacheBackendBlobReader(options.HttpClient, backend, options.Cache, options.MaxCacheSize)
	return &Server{
		meta:    pageMeta,
		options: &options,
		reader:  reader,
	}
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	err := s.Serve(writer, request)
	if errors.Is(err, os.ErrNotExist) {
		http.Error(writer, "page not found.", http.StatusNotFound)
	} else {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) Serve(writer http.ResponseWriter, request *http.Request) error {
	meta, err := s.meta.ParseDomainMeta(request.Host, request.URL.Path, request.URL.Query().Get("branch"))
	if err != nil {
		return err
	}
	result, err := s.reader.Open(meta.Owner, meta.Repo, meta.CommitID, meta.Path)
	if err != nil {
		return err
	}
	fileName := filepath.Base(meta.Path)
	if reader, ok := result.(*utils.CacheContent); ok {
		writer.Header().Add("X-Cache", "HIT")
		http.ServeContent(writer, request, fileName, reader.LastModified, reader)
		_ = reader.Close()
	} else {
		writer.Header().Add("X-Cache", "MISS")
		writer.Header().Set("Content-Type", mime.TypeByExtension(meta.Path))
		writer.WriteHeader(http.StatusOK)
		_, _ = io.Copy(writer, result)
		_ = result.Close()
	}
	return nil
}

func (s *Server) Close() error {
	if err := s.options.Config.Close(); err != nil {
		return err
	}
	if err := s.options.Cache.Close(); err != nil {
		return err
	}
	return nil
}
