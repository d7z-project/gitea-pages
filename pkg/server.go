package pkg

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
	db        kv.CursorPagedKV
	filterMgr map[string]core.FilterInstance

	globCache *lru.Cache[string, glob.Glob]

	cacheBlob    cache.Cache
	cacheBlobTTL time.Duration

	event        subscribe.Subscriber
	errorHandler func(w http.ResponseWriter, r *http.Request, err error)
}

func NewPageServer(
	client *http.Client,
	backend core.Backend,
	domain string,
	defaultBranch string,
	db kv.CursorPagedKV,
	event subscribe.Subscriber,
	cacheMeta kv.KV,
	cacheMetaTTL time.Duration,
	cacheBlob cache.Cache,
	cacheBlobTTL time.Duration,
	errorHandler func(w http.ResponseWriter, r *http.Request, err error),
	filterConfig map[string]map[string]any,
) (*Server, error) {
	alias := core.NewDomainAlias(db.Child("config", "alias"))
	svcMeta := core.NewServerMeta(client, backend, domain, alias, cacheMeta, cacheMetaTTL)
	pageMeta := core.NewPageDomain(svcMeta, domain, defaultBranch)
	globCache, err := lru.New[string, glob.Glob](512)
	if err != nil {
		return nil, err
	}
	defaultFilters, err := filters.DefaultFilters(filterConfig)
	if err != nil {
		return nil, err
	}
	return &Server{
		backend:      backend,
		meta:         pageMeta,
		db:           db,
		globCache:    globCache,
		filterMgr:    defaultFilters,
		errorHandler: errorHandler,
		cacheBlob:    cacheBlob,
		cacheBlobTTL: cacheBlobTTL,
		event:        event,
	}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	sessionID, _ := uuid.NewRandom()
	request.Header.Set("Session-ID", sessionID.String())
	writer := utils.NewWrittenResponseWriter(w)
	defer func() {
		if e := recover(); e != nil {
			zap.L().Error("panic!", zap.Any("error", e), zap.Any("id", sessionID))
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
		zap.L().Debug("bad request", zap.Error(err), zap.Any("request", request.RequestURI), zap.Any("id", sessionID))
		if !writer.IsWritten() {
			s.errorHandler(writer, request, err)
		}
	}
}

func (s *Server) Serve(writer *utils.WrittenResponseWriter, request *http.Request) error {
	ctx := request.Context()
	domain := portExp.ReplaceAllString(strings.ToLower(request.Host), "")
	meta, err := s.meta.ParseDomainMeta(ctx, domain, request.URL.Path, request.URL.Query().Get("branch"))
	if err != nil {
		return err
	}

	cancel, cancelFunc := context.WithCancel(request.Context())
	filterCtx := core.FilterContext{
		PageContent: meta,
		Context:     cancel,
		PageVFS:     core.NewPageVFS(s.backend, meta.Owner, meta.Repo, meta.CommitID),
		Cache:       tools.NewTTLCache(s.cacheBlob.Child("filter", meta.Owner, meta.Repo, meta.CommitID), time.Minute),
		OrgDB:       s.db.Child("org", meta.Owner).(kv.CursorPagedKV),
		RepoDB:      s.db.Child("repo", meta.Owner, meta.Repo).(kv.CursorPagedKV),
		Event:       s.event.Child("domain", meta.Owner, meta.Repo),

		Kill: cancelFunc,
	}

	zap.L().Debug("new request", zap.Any("request path", meta.Path))

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

	l := len(filtersRoute)
	for i := l - 2; i >= 0; i-- {
		filtersRoute = append(filtersRoute, filtersRoute[i])
	}
	zap.L().Debug("active filters", zap.String("filters", strings.Join(filtersRoute, " -> ")))

	var stack core.NextCall = core.NotFountNextCall
	for i, filter := range activeFiltersCall {
		stack = core.NextCallWrapper(filter, stack, activeFilters[i])
	}
	err = stack(filterCtx, writer, request)
	return err
}
