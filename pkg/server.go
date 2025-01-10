package pkg

import (
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"code.d7z.net/d7z-project/gitea-pages/pkg/core"
	"code.d7z.net/d7z-project/gitea-pages/pkg/utils"
	"github.com/pbnjay/memory"
)

type ServerOptions struct {
	Domain        string
	DefaultBranch string

	Config utils.Config
	Cache  utils.Cache

	MaxCacheSize int

	HttpClient *http.Client

	DefaultErrorHandler func(w http.ResponseWriter, r *http.Request, err error)
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
		DefaultErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, os.ErrNotExist) {
				http.Error(w, "page not found.", http.StatusNotFound)
			} else if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		},
	}
}

type Server struct {
	options *ServerOptions
	meta    *core.PageDomain
	reader  *core.CacheBackendBlobReader
}

func NewPageServer(backend core.Backend, options ServerOptions) *Server {
	backend = core.NewCacheBackend(backend, options.Config, time.Minute)
	svcMeta := core.NewServerMeta(options.HttpClient, backend, options.Config, time.Minute)
	pageMeta := core.NewPageDomain(svcMeta, options.Config, options.Domain, options.DefaultBranch)
	reader := core.NewCacheBackendBlobReader(options.HttpClient, backend, options.Cache, options.MaxCacheSize)
	return &Server{
		options: &options,
		meta:    pageMeta,
		reader:  reader,
	}
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	defer func() {
		if e := recover(); e != nil {
			zap.L().Error("panic!", zap.Any("error", e))
			if err, ok := e.(error); ok {
				s.options.DefaultErrorHandler(writer, request, err)
			}
		}
	}()
	err := s.Serve(writer, request)
	if err != nil {
		s.options.DefaultErrorHandler(writer, request, err)
	}
}

func (s *Server) Serve(writer http.ResponseWriter, request *http.Request) error {
	meta, err := s.meta.ParseDomainMeta(request.Host, request.URL.Path, request.URL.Query().Get("branch"))
	if err != nil {
		return err
	}
	zap.L().Debug("获取请求", zap.Any("meta", meta))
	// todo(feat) : 支持 http range
	result, err := s.reader.Open(meta.Owner, meta.Repo, meta.CommitID, meta.Path)
	if err != nil {
		if meta.HistoryRouteMode && errors.Is(err, os.ErrNotExist) {
			result, err = s.reader.Open(meta.Owner, meta.Repo, meta.CommitID, "index.html")
		} else {
			return err
		}
	}
	if err != nil && meta.CustomNotFound && errors.Is(err, os.ErrNotExist) {
		// 存在 404 页面的情况
		result, err = s.reader.Open(meta.Owner, meta.Repo, meta.CommitID, "404.html")
		if err != nil {
			return err
		}
		writer.Header().Set("Content-Type", mime.TypeByExtension(".html"))
		writer.WriteHeader(http.StatusNotFound)
		_, _ = io.Copy(writer, result)
		_ = result.Close()
		return nil
	}
	fileName := filepath.Base(meta.Path)
	if reader, ok := result.(*utils.CacheContent); ok {
		writer.Header().Add("X-Cache", "HIT")
		writer.Header().Add("Cache-Control", "public, max-age=86400")
		http.ServeContent(writer, request, fileName, reader.LastModified, reader)
		_ = reader.Close()
	} else {
		if reader, ok := result.(*utils.SizeReadCloser); ok {
			writer.Header().Add("Content-Length", strconv.Itoa(reader.Size))
		}
		// todo(bug) : 直连模式下告知数据长度
		writer.Header().Add("X-Cache", "MISS")
		writer.Header().Add("Cache-Control", "public, max-age=86400")
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
