package pkg

import (
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/pbnjay/memory"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/gitea-pages/pkg/utils"

	_ "gopkg.d7z.net/gitea-pages/pkg/renders"
)

var portExp = regexp.MustCompile(`:\d+$`)

type ServerOptions struct {
	Domain        string
	DefaultBranch string

	KVConfig utils.KVConfig
	Cache    utils.Cache

	MaxCacheSize int

	HttpClient *http.Client

	MetaTTL time.Duration

	EnableRender bool
	EnableProxy  bool

	DefaultErrorHandler func(w http.ResponseWriter, r *http.Request, err error)
}

func DefaultOptions(domain string) ServerOptions {
	configMemory, _ := utils.NewAutoConfig("")
	return ServerOptions{
		Domain:        domain,
		DefaultBranch: "gh-pages",
		KVConfig:      configMemory,
		Cache:         utils.NewCacheMemory(1024*1024*10, int(memory.FreeMemory()/3*2)),
		MaxCacheSize:  1024 * 1024 * 10,
		HttpClient:    http.DefaultClient,
		MetaTTL:       time.Minute,
		EnableRender:  true,
		EnableProxy:   true,
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
	backend core.Backend
}

func NewPageServer(backend core.Backend, options ServerOptions) *Server {
	backend = core.NewCacheBackend(backend, options.KVConfig, options.MetaTTL)
	svcMeta := core.NewServerMeta(options.HttpClient, backend, options.KVConfig, options.Domain, options.MetaTTL)
	pageMeta := core.NewPageDomain(svcMeta, options.KVConfig, options.Domain, options.DefaultBranch)
	reader := core.NewCacheBackendBlobReader(options.HttpClient, backend, options.Cache, options.MaxCacheSize)
	return &Server{
		backend: backend,
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
	domainHost := portExp.ReplaceAllString(strings.ToLower(request.Host), "")
	meta, err := s.meta.ParseDomainMeta(
		domainHost,
		request.URL.Path,
		request.URL.Query().Get("branch"))
	if err != nil {
		return err
	}
	zap.L().Debug("获取请求", zap.Any("request", meta.Path))
	if len(meta.Alias) > 0 && !slices.Contains(meta.Alias, domainHost) {
		zap.L().Debug("重定向地址", zap.Any("src", request.Host), zap.Any("dst", meta.Alias[0]))
		http.Redirect(writer, request, fmt.Sprintf("https://%s/%s", meta.Alias[0], meta.Path), http.StatusFound)
		return nil
	}

	if s.options.EnableProxy {
		for prefix, backend := range meta.Proxy {
			proxyPath := "/" + meta.Path
			if strings.HasPrefix(proxyPath, prefix) {
				targetPath := strings.TrimPrefix(proxyPath, prefix)
				if !strings.HasPrefix(targetPath, "/") {
					targetPath = "/" + targetPath
				}
				u, _ := url.Parse(backend)
				request.URL.Path = targetPath
				request.RequestURI = request.URL.RequestURI()
				proxy := httputil.NewSingleHostReverseProxy(u)
				proxy.Transport = s.options.HttpClient.Transport

				if host, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
					request.Header.Set("X-Real-IP", host)
				}
				request.Header.Set("X-Page-IP", utils.GetRemoteIP(request))
				request.Header.Set("X-Page-Refer", fmt.Sprintf("%s/%s/%s", meta.Owner, meta.Repo, meta.Path))
				request.Header.Set("X-Page-Host", request.Host)
				zap.L().Debug("命中反向代理", zap.Any("prefix", prefix), zap.Any("backend", backend),
					zap.Any("path", proxyPath), zap.Any("target", fmt.Sprintf("%s%s", u, targetPath)))
				// todo(security): 处理 websocket
				proxy.ServeHTTP(writer, request)
				return nil
			}
		}
	}
	// 在非反向代理时处理目录访问
	if strings.HasSuffix(meta.Path, "/") || meta.Path == "" {
		meta.Path = meta.Path + "index.html"
	}

	// 如果不是反向代理路由则跳过任何配置
	if request.Method != "GET" {
		return os.ErrNotExist
	}
	var result io.ReadCloser
	if meta.IgnorePath(meta.Path) {
		zap.L().Debug("ignore path", zap.Any("request", request.RequestURI), zap.Any("meta.path", meta.Path))
		err = os.ErrNotExist
	} else {
		result, err = s.reader.Open(meta.Owner, meta.Repo, meta.CommitID, meta.Path)
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if meta.VRoute {
				// 回退 abc => index.html
				result, err = s.reader.Open(meta.Owner, meta.Repo, meta.CommitID, "index.html")
				if err == nil {
					meta.Path = "index.html"
				}
			} else {
				// 回退 abc => abc/ => abc/index.html
				result, err = s.reader.Open(meta.Owner, meta.Repo, meta.CommitID, meta.Path+"/index.html")
				if err == nil {
					meta.Path = strings.Trim(meta.Path+"/index.html", "/")
				}
			}
		} else {
			return err
		}
	}
	// 处理请求错误
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result, err = s.reader.Open(meta.Owner, meta.Repo, meta.CommitID, "404.html")
			if err != nil {
				return err
			}
			writer.Header().Set("Content-Type", mime.TypeByExtension(".html"))
			writer.WriteHeader(http.StatusNotFound)
			if render := meta.TryRender(meta.Path, "/404.html"); render != nil && s.options.EnableRender {
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
		} else {
			return err
		}
	}
	fileName := filepath.Base(meta.Path)
	render := meta.TryRender(meta.Path)
	if !s.options.EnableRender {
		render = nil
	}
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
	if err := s.options.KVConfig.Close(); err != nil {
		return err
	}
	if err := s.options.Cache.Close(); err != nil {
		return err
	}
	return s.backend.Close()
}
