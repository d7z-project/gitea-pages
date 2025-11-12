package pkg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/gitea-pages/pkg/utils"
	"gopkg.d7z.net/middleware/cache"
	"gopkg.d7z.net/middleware/kv"
)

var portExp = regexp.MustCompile(`:\d+$`)

type ServerOptions struct {
	Domain        string // 默认域名
	DefaultBranch string // 默认分支

	Alias kv.KV // 配置映射关系

	CacheMeta    kv.KV         // 配置缓存
	CacheMetaTTL time.Duration // 配置缓存时长

	CacheBlob    cache.Cache   // blob缓存
	CacheBlobTTL time.Duration // 配置缓存时长
	CacheControl string        // 缓存配置

	CacheBlobLimit uint64 // blob最大缓存大小

	HTTPClient   *http.Client // 自定义客户端
	EnableRender bool         // 允许渲染

	EnableProxy bool // 允许代理

	StaticDir           string // 静态文件位置
	DefaultErrorHandler func(w http.ResponseWriter, r *http.Request, err error)
}

func DefaultOptions(domain string) ServerOptions {
	configMemory, _ := kv.NewMemory("")
	cacheMemory, _ := cache.NewMemoryCache(cache.MemoryCacheConfig{MaxCapacity: 4096, CleanupInt: time.Hour})
	return ServerOptions{
		Domain:        domain,
		DefaultBranch: "gh-pages",

		Alias:        configMemory,
		CacheMeta:    configMemory,
		CacheMetaTTL: time.Minute,

		CacheBlob:      cacheMemory,
		CacheBlobTTL:   time.Minute,
		CacheBlobLimit: 1024 * 1024 * 10,
		CacheControl:   "public, max-age=86400",

		HTTPClient: http.DefaultClient,

		EnableRender: true,
		EnableProxy:  true,
		StaticDir:    "",
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
	backend core.Backend
	fs      http.Handler
}

var staticPrefix = "/.well-known/page-server/"

func NewPageServer(backend core.Backend, options ServerOptions) *Server {
	svcMeta := core.NewServerMeta(options.HTTPClient, backend, options.CacheMeta, options.Domain, options.CacheMetaTTL)
	pageMeta := core.NewPageDomain(svcMeta, core.NewDomainAlias(options.Alias), options.Domain, options.DefaultBranch)
	var fs http.Handler
	if options.StaticDir != "" {
		fs = http.StripPrefix(staticPrefix, http.FileServer(http.Dir(options.StaticDir)))
	}
	return &Server{
		backend: backend,
		options: &options,
		meta:    pageMeta,
		fs:      fs,
	}
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	sessionID, _ := uuid.NewRandom()
	request.Header.Set("Session-ID", sessionID.String())
	if s.fs != nil && strings.HasPrefix(request.URL.Path, staticPrefix) {
		s.fs.ServeHTTP(writer, request)
		return
	}
	defer func() {
		if e := recover(); e != nil {
			zap.L().Error("panic!", zap.Any("error", e), zap.Any("id", sessionID))
			if err, ok := e.(error); ok {
				s.options.DefaultErrorHandler(writer, request, err)
			}
		}
	}()
	err := s.Serve(writer, request)
	if err != nil {
		zap.L().Debug("错误请求", zap.Error(err), zap.Any("request", request.RequestURI), zap.Any("id", sessionID))
		s.options.DefaultErrorHandler(writer, request, err)
	}
}

func (s *Server) Serve(writer http.ResponseWriter, request *http.Request) error {
	ctx := request.Context()
	domain := portExp.ReplaceAllString(strings.ToLower(request.Host), "")
	meta, err := s.meta.ParseDomainMeta(ctx, domain, request.URL.Path, request.URL.Query().Get("branch"))
	if err != nil {
		return err
	}
	zap.L().Debug("new request", zap.Any("request path", meta.Path))
	if len(meta.Alias) > 0 && !slices.Contains(meta.Alias, domain) {
		// 重定向到配置的地址
		zap.L().Debug("redirect", zap.Any("src", request.Host), zap.Any("dst", meta.Alias[0]))
		http.Redirect(writer, request, fmt.Sprintf("https://%s/%s", meta.Alias[0], meta.Path), http.StatusFound)
		return nil
	}
	if s.options.EnableProxy && s.Proxy(writer, request, meta) {
		return nil
	}

	if strings.HasSuffix(meta.Path, "/") || meta.Path == "" {
		meta.Path += "index.html"
	}
	// 如果不是反向代理路由则跳过任何配置
	if request.Method != http.MethodGet {
		return os.ErrNotExist
	}
	if meta.IgnorePath(meta.Path) {
		zap.L().Debug("ignore path", zap.Any("request", request.RequestURI), zap.Any("meta.path", meta.Path))
		return os.ErrNotExist
	}

	var resp *http.Response
	var path string
	failback := []string{meta.Path, meta.Path + "/index.html"}
	if meta.VRoute {
		failback = append(failback, "index.html")
	}
	failback = append(failback, "404.html")
	for _, p := range failback {
		resp, err = meta.NativeOpen(request.Context(), p, nil)
		if err != nil {
			if resp != nil {
				resp.Body.Close()
			}
			if !errors.Is(err, os.ErrNotExist) {
				zap.L().Debug("error", zap.Any("error", err))
			}
			continue
		}
		path = p
		break
	}

	if resp == nil {
		return os.ErrNotExist
	}
	defer resp.Body.Close()

	if err != nil {
		return err
	}
	if path == "404.html" && request.URL.Path != "/404.html" {
		writer.WriteHeader(http.StatusNotFound)
	}
	ctx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
	defer cancel()
	if render := meta.TryRender(path); render != nil {
		return render.Render(ctx, writer, request, resp.Body, meta)
	}
	writer.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	lastMod, err := time.Parse(http.TimeFormat, resp.Header.Get("Last-Modified"))
	if err == nil {
		if seeker, ok := resp.Body.(io.ReadSeeker); ok && !(path == "404.html" && request.URL.Path != "/404.html") {
			http.ServeContent(writer, request, filepath.Base(path), lastMod, seeker)
			return nil
		}
	}
	_, err = io.Copy(writer, resp.Body)
	return err
}

func (s *Server) Proxy(writer http.ResponseWriter, request *http.Request, meta *core.PageDomainContent) bool {
	proxyPath := "/" + meta.Path
	for prefix, backend := range meta.Proxy {
		if strings.HasPrefix(proxyPath, prefix) {
			targetPath := strings.TrimPrefix(proxyPath, prefix)
			if !strings.HasPrefix(targetPath, "/") {
				targetPath = "/" + targetPath
			}
			u, _ := url.Parse(backend)
			request.URL.Path = targetPath
			request.RequestURI = request.URL.RequestURI()
			proxy := httputil.NewSingleHostReverseProxy(u)
			proxy.Transport = s.options.HTTPClient.Transport

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
			return true
		}
	}
	return false
}

func (s *Server) Close() error {
	return errors.Join(
		s.options.CacheBlob.Close(),
		s.options.CacheMeta.Close(),
		s.options.Alias.Close(),
		s.backend.Close(),
	)
}
