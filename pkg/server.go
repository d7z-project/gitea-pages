package pkg

import (
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
	"gopkg.d7z.net/middleware/kv"
)

var portExp = regexp.MustCompile(`:\d+$`)

type Server struct {
	backend core.Backend
	meta    *core.PageDomain

	errorHandler func(w http.ResponseWriter, r *http.Request, err error)

	filterMgr map[string]core.FilterInstance
	globCache *lru.Cache[string, glob.Glob]
}

func NewPageServer(
	client *http.Client,
	backend core.Backend,
	domain string,
	defaultBranch string,
	db kv.KV,
	cache kv.KV,
	cacheTTL time.Duration,
	errorHandler func(w http.ResponseWriter, r *http.Request, err error),
) *Server {
	svcMeta := core.NewServerMeta(client, backend, domain, cache, cacheTTL)
	cfgDB := db.Child("config")
	pageMeta := core.NewPageDomain(svcMeta,
		core.NewDomainAlias(cfgDB.Child("alias")),
		cfgDB.Child("pages"),
		domain, defaultBranch)
	c, err := lru.New[string, glob.Glob](256)
	if err != nil {
		panic(err)
	}
	return &Server{
		backend:      backend,
		meta:         pageMeta,
		globCache:    c,
		filterMgr:    filters.DefaultFilters(),
		errorHandler: errorHandler,
	}
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	sessionID, _ := uuid.NewRandom()
	request.Header.Set("Session-ID", sessionID.String())
	//if s.staticFS != nil && strings.HasPrefix(request.URL.Path, staticPrefix) {
	//	s.staticFS.ServeHTTP(writer, request)
	//	return
	//}
	defer func() {
		if e := recover(); e != nil {
			zap.L().Error("panic!", zap.Any("error", e), zap.Any("id", sessionID))
			if err, ok := e.(error); ok {
				s.errorHandler(writer, request, err)
			}
		}
	}()
	err := s.Serve(writer, request)
	if err != nil {
		zap.L().Debug("错误请求", zap.Error(err), zap.Any("request", request.RequestURI), zap.Any("id", sessionID))
		s.errorHandler(writer, request, err)
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
	err = stack(ctx, writer, request, meta)
	return err
}
