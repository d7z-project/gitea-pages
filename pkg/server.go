package pkg

import (
	"errors"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gobwas/glob"
	"github.com/google/uuid"
	lru "github.com/hashicorp/golang-lru/v2"
	"go.uber.org/zap"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/gitea-pages/pkg/filters"
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
	options   *ServerOptions
	meta      *core.PageDomain
	backend   core.Backend
	fs        http.Handler
	filterMgr map[string]core.FilterInstance

	filtersCache *lru.Cache[string, glob.Glob]
}

var staticPrefix = "/.well-known/page-server/"

func NewPageServer(backend core.Backend, options ServerOptions) *Server {
	svcMeta := core.NewServerMeta(options.HTTPClient, backend, options.CacheMeta, options.Domain, options.CacheMetaTTL)
	pageMeta := core.NewPageDomain(svcMeta, core.NewDomainAlias(options.Alias), options.Domain, options.DefaultBranch)
	var fs http.Handler
	if options.StaticDir != "" {
		fs = http.StripPrefix(staticPrefix, http.FileServer(http.Dir(options.StaticDir)))
	}
	c, err := lru.New[string, glob.Glob](256)
	if err != nil {
		panic(err)
	}
	return &Server{
		backend:      backend,
		options:      &options,
		meta:         pageMeta,
		fs:           fs,
		filtersCache: c,
		filterMgr:    filters.DefaultFilters(),
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

	if strings.HasSuffix(meta.Path, "/") || meta.Path == "" {
		meta.Path += "index.html"
	}
	activeFiltersCall := make([]core.FilterCall, 0)
	activeFilters := make([]core.Filter, 0)

	for _, filter := range meta.Filters {
		value, ok := s.filtersCache.Get(filter.Path)
		if !ok {
			value, err = glob.Compile(filter.Path)
			if err != nil {
				continue
			}
			s.filtersCache.Add(filter.Path, value)
		}
		if value.Match(meta.Path) {
			instance := s.filterMgr[filter.Type]
			if instance == nil {
				return errors.New("filter not found : " + filter.Type)
			}
			activeFilters = append(activeFilters, filter)
			call, err := instance(filter.Params)
			if err != nil {
				return err
			}
			activeFiltersCall = append(activeFiltersCall, call)
		}
	}
	slices.Reverse(activeFiltersCall)
	slices.Reverse(activeFilters)

	zap.L().Debug("active filters", zap.Any("filters", activeFilters))

	direct, _ := filters.FilterInstDirect(map[string]any{
		"prefix": "",
	})
	stack := core.NextCallWrapper(direct, nil)
	for _, filter := range activeFiltersCall {
		stack = core.NextCallWrapper(filter, stack)
	}
	return stack(ctx, writer, request, meta)
}

func (s *Server) Close() error {
	return errors.Join(
		s.options.CacheBlob.Close(),
		s.options.CacheMeta.Close(),
		s.options.Alias.Close(),
		s.backend.Close(),
	)
}
