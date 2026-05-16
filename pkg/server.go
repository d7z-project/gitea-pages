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
	mwstorage "gopkg.d7z.net/middleware/storage"
	"gopkg.d7z.net/middleware/subscribe"
	"gopkg.d7z.net/middleware/tools"
)

var portExp = regexp.MustCompile(`:\d+$`)

type Server struct {
	backend      core.Backend
	meta         *core.PageDomain
	db           kv.KV
	userDB       kv.KV
	filterMgr    map[string]core.FilterInstance
	trustedProxy *core.TrustedProxyPolicy

	globCache *lru.Cache[string, glob.Glob]

	cacheBlob    cache.Cache
	cacheBlobTTL time.Duration

	storage      mwstorage.Storage
	event        subscribe.Subscriber
	updateHub    *core.RepoUpdateHub
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
	storage                    mwstorage.Storage
	errorHandler               func(w http.ResponseWriter, r *http.Request, err error)
	filterConfig               map[string]map[string]any
	trustedProxies             []string
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

func WithStorage(storage mwstorage.Storage) ServerOption {
	return func(c *serverConfig) {
		c.storage = storage
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

func WithTrustedProxies(entries []string) ServerOption {
	return func(c *serverConfig) {
		c.trustedProxies = append([]string(nil), entries...)
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
	if cfg.storage == nil {
		cfg.storage = mwstorage.NewMemoryStorage()
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
	updateHub := core.NewRepoUpdateHub(cfg.event)
	globCache, err := lru.New[string, glob.Glob](512)
	if err != nil {
		return nil, err
	}
	defaultFilters, err := filters.DefaultFilters(cfg.filterConfig)
	if err != nil {
		return nil, err
	}
	enabledFilters := make([]string, 0, len(defaultFilters))
	for name := range defaultFilters {
		enabledFilters = append(enabledFilters, name)
	}
	svcMeta := core.NewServerMeta(
		cfg.client,
		backend,
		domain,
		alias,
		cfg.cacheMeta,
		cfg.cacheMetaTTL,
		cfg.cacheMetaRefresh,
		cfg.cacheMetaRefreshConcurrent,
		enabledFilters,
		updateHub,
	)
	pageMeta := core.NewPageDomain(svcMeta, domain)
	var trustedProxy *core.TrustedProxyPolicy
	if len(cfg.trustedProxies) > 0 {
		trustedProxy, err = core.NewTrustedProxyPolicy(cfg.trustedProxies)
		if err != nil {
			return nil, err
		}
	}
	return &Server{
		backend:      backend,
		meta:         pageMeta,
		db:           db,
		userDB:       userDB,
		globCache:    globCache,
		filterMgr:    defaultFilters,
		trustedProxy: trustedProxy,
		errorHandler: cfg.errorHandler,
		cacheBlob:    cfg.cacheBlob,
		cacheBlobTTL: cfg.cacheBlobTTL,
		storage:      cfg.storage,
		event:        cfg.event,
		updateHub:    updateHub,
		auth:         cfg.authService,
	}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	sessionID, _ := uuid.NewRandom()
	request.Header.Set("Session-ID", sessionID.String())
	requestInfo := core.ResolveRequestInfo(request, s.trustedProxy)
	request = request.WithContext(core.ContextWithRequestInfo(request.Context(), requestInfo))
	var meta *core.PageContent
	var err error
	if !core.IsReservedPath(request.URL.Path) {
		domain := portExp.ReplaceAllString(strings.ToLower(request.Host), "")
		meta, err = s.meta.ParseDomainMeta(request.Context(), domain, request.URL.Path)
		if err != nil {
			s.handleRequestError(w, request, sessionID, err)
			return
		}
	}
	securityConfig := core.DefaultPageSecurity()
	if meta != nil {
		securityConfig = meta.Security
	}
	security := core.BuildSecurityResult(request, requestInfo, securityConfig)
	applyRequestSecurity(request, security)
	if enforceRequestSecurity(w, request, security) {
		return
	}
	trackedWriter := utils.NewWrittenResponseWriter(w)
	writer := &securityResponseWriter{ResponseWriter: trackedWriter, security: security}
	defer func() {
		if e := recover(); e != nil {
			slog.Error("panic!", "error", e, "id", sessionID)
			if !trackedWriter.IsWritten() {
				if err, ok := e.(error); ok {
					s.errorHandler(writer, request, err)
				} else {
					s.errorHandler(writer, request, errors.New("panic"))
				}
			}
		}
	}()
	err = s.servePage(writer, request, meta)
	if err != nil {
		s.handleRequestError(writer, request, sessionID, err)
	}
}

func (s *Server) handleRequestError(writer http.ResponseWriter, request *http.Request, sessionID uuid.UUID, err error) {
	slog.Debug("bad request", "error", err, "request", request.RequestURI, "id", sessionID)
	if utils.IsWrittenResponseWriter(writer) {
		return
	}
	s.errorHandler(writer, request, err)
}

func (s *Server) servePage(writer http.ResponseWriter, request *http.Request, meta *core.PageContent) error {
	if core.IsReservedPath(request.URL.Path) {
		if s.auth == nil {
			http.NotFound(writer, request)
			return nil
		}
		return s.auth.Handle(writer, request)
	}
	var err error
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
	releaseUpdate, err := s.updateHub.Attach(meta.Owner, meta.Repo, meta.CommitID, request.Header.Get("Session-ID"), cancelFunc)
	if err != nil {
		return err
	}
	defer releaseUpdate()
	repoStorage := s.storage.Child("repo", meta.Owner, meta.Repo)
	if err = repoStorage.MkdirAll(".", 0o755); err != nil {
		return err
	}
	filterCtx := core.FilterContext{
		PageContent:  meta,
		Context:      cancelCtx,
		PageVFS:      core.NewPageVFS(s.backend, meta.Owner, meta.Repo, meta.CommitID),
		Cache:        tools.NewTTLCache(s.cacheBlob.Child("filter", meta.Owner, meta.Repo, meta.CommitID), s.cacheBlobTTL),
		OrgDB:        s.userDB.Child("org", meta.Owner),
		RepoDB:       s.userDB.Child("repo", meta.Owner, meta.Repo),
		Storage:      repoStorage,
		VersionEvent: s.event.Child("version", meta.Owner, meta.Repo, meta.CommitID),
		SharedEvent:  s.event.Child("shared", meta.Owner, meta.Repo),
		Auth:         core.AuthInfoFromContext(request.Context()),

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
				return fmt.Errorf("filter %q became unavailable after metadata validation", filter.Type)
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
