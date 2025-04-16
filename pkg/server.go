package pkg

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/pbnjay/memory"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/gitea-pages/pkg/utils"
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
	sessionId, _ := uuid.NewRandom()
	request.Header.Set("Session-ID", sessionId.String())
	defer func() {
		if e := recover(); e != nil {
			zap.L().Error("panic!", zap.Any("error", e), zap.Any("id", sessionId))
			if err, ok := e.(error); ok {
				s.options.DefaultErrorHandler(writer, request, err)
			}
		}
	}()
	err := s.Serve(writer, request)
	if err != nil {
		zap.L().Debug("错误请求", zap.Error(err), zap.Any("request", request.RequestURI), zap.Any("id", sessionId))
		s.options.DefaultErrorHandler(writer, request, err)
	}
}

func (s *Server) Serve(writer http.ResponseWriter, request *http.Request) error {
	if request.Method != "GET" {
		return os.ErrNotExist
	}
	meta, err := s.meta.ParseDomainMeta(request.Host, request.URL.Path, request.URL.Query().Get("branch"))
	if err != nil {
		return err
	}
	zap.L().Debug("获取请求", zap.Any("request", meta.Path))
	// todo(feat) : 支持 http range
	if meta.Domain != "" && meta.Domain != request.Host {
		zap.L().Debug("重定向地址", zap.Any("src", request.Host), zap.Any("dst", meta.Domain))
		http.Redirect(writer, request, fmt.Sprintf("https://%s/%s", meta.Domain, meta.Path), http.StatusFound)
		return nil
	}
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
		if render := meta.TryRender(meta.Path, "/404.html"); render != nil {
			defer result.Close()
			if err = render.Render(writer, request, result); err != nil {
				return err
			}
			return nil
		} else {
			_, _ = io.Copy(writer, result)
			_ = result.Close()
		}
		return nil
	}
	fileName := filepath.Base(meta.Path)
	render := meta.TryRender(meta.Path)
	defer result.Close()
	if reader, ok := result.(*utils.CacheContent); ok {
		writer.Header().Add("X-Cache", "HIT")
		writer.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(fileName)))
		writer.Header().Add("Cache-Control", "public, max-age=86400")
		if render != nil {
			if err = render.Render(writer, request, reader); err != nil {
				return err
			}
		} else {
			http.ServeContent(writer, request, fileName, reader.LastModified, reader)
		}
	} else {
		if reader, ok := result.(*utils.SizeReadCloser); ok && render == nil {
			writer.Header().Add("Content-Length", strconv.Itoa(reader.Size))
		}
		// todo(bug) : 直连模式下告知数据长度
		writer.Header().Add("X-Cache", "MISS")
		writer.Header().Add("Cache-Control", "public, max-age=86400")
		writer.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(fileName)))
		writer.WriteHeader(http.StatusOK)
		if render != nil {
			if err = render.Render(writer, request, reader); err != nil {
				return err
			}
		} else {
			_, _ = io.Copy(writer, result)
		}
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
