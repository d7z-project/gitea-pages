package pkg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gobwas/glob"
	"github.com/google/uuid"
	lru "github.com/hashicorp/golang-lru/v2"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/gitea-pages/pkg/filters"
	"gopkg.d7z.net/gitea-pages/pkg/utils"
	"gopkg.d7z.net/middleware/cache"
	"gopkg.d7z.net/middleware/kv"
	"gopkg.d7z.net/middleware/subscribe"
	"gopkg.d7z.net/middleware/tools"
)

var portExp = regexp.MustCompile(`:\d+$`)

type Server struct {
	backend   core.Backend
	meta      *core.PageDomain
	db        kv.KV
	userDB    kv.KV
	filterMgr map[string]core.FilterInstance

	globCache *lru.Cache[string, glob.Glob]

	cacheBlob    cache.Cache
	cacheBlobTTL time.Duration

	event        subscribe.Subscriber
	auth         *core.AuthService
	errorHandler func(w http.ResponseWriter, r *http.Request, err error)
}

type serverConfig struct {
	client                     *http.Client
	event                      subscribe.Subscriber
	cacheMeta                  kv.KV
	cacheMetaTTL               time.Duration
	cacheMetaRefresh           time.Duration
	cacheMetaRefreshConcurrent int
	cacheBlob                  cache.Cache
	cacheBlobTTL               time.Duration
	errorHandler               func(w http.ResponseWriter, r *http.Request, err error)
	filterConfig               map[string]map[string]any
	authService                *core.AuthService
}

type ServerOption func(*serverConfig)

func WithClient(client *http.Client) ServerOption {
	return func(c *serverConfig) {
		c.client = client
	}
}

func WithEvent(event subscribe.Subscriber) ServerOption {
	return func(c *serverConfig) {
		c.event = event
	}
}

func WithMetaCache(cache kv.KV, ttl, refresh time.Duration, refreshConcurrent int) ServerOption {
	return func(c *serverConfig) {
		c.cacheMeta = cache
		c.cacheMetaTTL = ttl
		c.cacheMetaRefresh = refresh
		c.cacheMetaRefreshConcurrent = refreshConcurrent
	}
}

func WithBlobCache(cache cache.Cache, ttl time.Duration) ServerOption {
	return func(c *serverConfig) {
		c.cacheBlob = cache
		c.cacheBlobTTL = ttl
	}
}

func WithErrorHandler(handler func(w http.ResponseWriter, r *http.Request, err error)) ServerOption {
	return func(c *serverConfig) {
		c.errorHandler = handler
	}
}

func WithFilterConfig(config map[string]map[string]any) ServerOption {
	return func(c *serverConfig) {
		c.filterConfig = config
	}
}

func WithAuth(authService *core.AuthService) ServerOption {
	return func(c *serverConfig) {
		c.authService = authService
	}
}

func NewPageServer(
	backend core.Backend,
	domain string,
	db kv.KV,
	userDB kv.KV,
	opts ...ServerOption,
) (*Server, error) {
	cfg := &serverConfig{
		client:       http.DefaultClient,
		filterConfig: make(map[string]map[string]any),
	}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.event == nil {
		cfg.event = subscribe.NewMemorySubscriber()
	}

	if cfg.cacheMeta == nil {
		var err error
		cfg.cacheMeta, err = kv.NewMemory("")
		if err != nil {
			return nil, err
		}
	}

	if cfg.cacheMetaRefresh == 0 {
		cfg.cacheMetaRefresh = cfg.cacheMetaTTL / 2
	}

	if cfg.cacheMetaRefreshConcurrent == 0 {
		cfg.cacheMetaRefreshConcurrent = 16
	}

	if cfg.cacheBlob == nil {
		var err error
		cfg.cacheBlob, err = cache.NewMemoryCache(cache.MemoryCacheConfig{
			MaxCapacity: 128,
			CleanupInt:  time.Minute,
		})
		if err != nil {
			return nil, err
		}
	}

	if cfg.errorHandler == nil {
		cfg.errorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
	if userDB == nil {
		userDB = db
	}

	alias := core.NewDomainAlias(db.Child("config", "alias"))
	svcMeta := core.NewServerMeta(cfg.client, backend, domain, alias, cfg.cacheMeta, cfg.cacheMetaTTL, cfg.cacheMetaRefresh, cfg.cacheMetaRefreshConcurrent)
	pageMeta := core.NewPageDomain(svcMeta, domain)
	globCache, err := lru.New[string, glob.Glob](512)
	if err != nil {
		return nil, err
	}
	defaultFilters, err := filters.DefaultFilters(cfg.filterConfig)
	if err != nil {
		return nil, err
	}
	return &Server{
		backend:      backend,
		meta:         pageMeta,
		db:           db,
		userDB:       userDB,
		globCache:    globCache,
		filterMgr:    defaultFilters,
		errorHandler: cfg.errorHandler,
		cacheBlob:    cfg.cacheBlob,
		cacheBlobTTL: cfg.cacheBlobTTL,
		event:        cfg.event,
		auth:         cfg.authService,
	}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	sessionID, _ := uuid.NewRandom()
	request.Header.Set("Session-ID", sessionID.String())
	writer := utils.NewWrittenResponseWriter(w)
	defer func() {
		if e := recover(); e != nil {
			slog.Error("panic!", "error", e, "id", sessionID)
			if !writer.IsWritten() {
				if err, ok := e.(error); ok {
					s.errorHandler(writer, request, err)
				} else {
					s.errorHandler(writer, request, errors.New("panic"))
				}
			}
		}
	}()
	err := s.Serve(writer, request)
	if err != nil {
		slog.Debug("bad request", "error", err, "request", request.RequestURI, "id", sessionID)
		if !writer.IsWritten() {
			s.errorHandler(writer, request, err)
		}
	}
}

func (s *Server) Serve(writer *utils.WrittenResponseWriter, request *http.Request) error {
	if core.IsReservedPath(request.URL.Path) {
		if s.auth == nil {
			http.NotFound(writer, request)
			return nil
		}
		return s.auth.Handle(writer, request)
	}
	ctx := request.Context()
	domain := portExp.ReplaceAllString(strings.ToLower(request.Host), "")
	meta, err := s.meta.ParseDomainMeta(ctx, domain, request.URL.Path)
	if err != nil {
		return err
	}
	if s.auth != nil {
		if err = s.auth.AttachAuth(request); err != nil {
			return err
		}
	}
	if meta.Private {
		if s.auth == nil {
			return errors.New("private repo requires auth provider")
		}
		allowed, authErr := s.auth.RequireRepoAccess(writer, request, meta)
		if authErr != nil || !allowed {
			return authErr
		}
	}
	writer.Header().Set("X-Page-ID", meta.CommitID)
	cancelCtx, cancelFunc := context.WithCancel(request.Context())
	filterCtx := core.FilterContext{
		PageContent: meta,
		Context:     cancelCtx,
		PageVFS:     core.NewPageVFS(s.backend, meta.Owner, meta.Repo, meta.CommitID),
		Cache:       tools.NewTTLCache(s.cacheBlob.Child("filter", meta.Owner, meta.Repo, meta.CommitID), s.cacheBlobTTL),
		OrgDB:       s.userDB.Child("org", meta.Owner),
		RepoDB:      s.userDB.Child("repo", meta.Owner, meta.Repo),
		Event:       s.event.Child("domain", meta.Owner, meta.Repo),
		Auth:        core.AuthInfoFromContext(request.Context()),

		Kill: cancelFunc,
	}

	slog.Debug("new request", "request path", meta.Path)

	if strings.HasSuffix(meta.Path, "/") || meta.Path == "" {
		meta.Path += "index.html"
	}
	activeFiltersCall := make([]core.FilterCall, 0)
	activeFilters := make([]core.Filter, 0)
	filtersRoute := make([]string, 0)

	for _, filter := range meta.Filters {
		value, ok := s.globCache.Get(filter.Path)
		if !ok {
			value, err = glob.Compile(filter.Path)
			if err != nil {
				slog.Warn("invalid glob pattern", "pattern", filter.Path, "error", err)
				continue
			}
			s.globCache.Add(filter.Path, value)
		}
		if value.Match(meta.Path) {
			instance := s.filterMgr[filter.Type]
			if instance == nil {
				return errors.New("filter not found : " + filter.Type)
			}
			activeFilters = append(activeFilters, filter)
			filtersRoute = append(filtersRoute, fmt.Sprintf("%s[%s]%s", filter.Type, filter.Path, filter.Params))
			call, err := instance(filter.Params)
			if err != nil {
				return err
			}
			activeFiltersCall = append(activeFiltersCall, call)
		}
	}
	slices.Reverse(activeFiltersCall)
	slices.Reverse(activeFilters)

	// Build the visual call stack for logging (e.g., A -> B -> C -> B -> A)
	l := len(filtersRoute)
	if l > 1 {
		for i := l - 2; i >= 0; i-- {
			filtersRoute = append(filtersRoute, filtersRoute[i])
		}
	}
	slog.Debug("active filters", "filters", strings.Join(filtersRoute, " -> "))

	var stack core.NextCall = core.NotFountNextCall
	for i, filter := range activeFiltersCall {
		stack = core.NextCallWrapper(filter, stack, activeFilters[i])
	}
	err = stack(filterCtx, writer, request)
	return err
}
