package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"gopkg.d7z.net/gitea-pages/pkg"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	_ "gopkg.d7z.net/gitea-pages/pkg/providers"
	"gopkg.d7z.net/gitea-pages/pkg/utils"
	"gopkg.d7z.net/middleware/cache"
	"gopkg.d7z.net/middleware/kv"
	"gopkg.d7z.net/middleware/storage"
	"gopkg.d7z.net/middleware/subscribe"
)

var (
	configPath = "config-local.yaml"
	debug      = false
)

func init() {
	flag.StringVar(&configPath, "conf", configPath, "config file path")
	flag.BoolVar(&debug, "debug", debug, "debug mode")
}

func main() {
	flag.Parse()
	logInject()
	config, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("fail to load config file: %v", err)
	}
	storageURL := strings.ToLower(strings.TrimSpace(config.Storage.URL))
	if strings.HasPrefix(storageURL, "memory://") || strings.HasPrefix(storageURL, "mem://") {
		slog.Warn("storage.url uses in-memory storage; data written by page scripts is kept in this server process's memory, is not released until the process restarts, and continued writes can exhaust memory", "storage", config.Storage.URL)
	}

	factory, ok := core.GetProviderFactory(config.Provider.Type)
	if !ok {
		log.Fatalf("unsupported provider type: %s", config.Provider.Type)
	}
	rawProviderConfig, ok := config.Provider.ProviderConfig(config.Provider.Type)
	if !ok {
		log.Fatalf("missing provider config for type: %s", config.Provider.Type)
	}
	provider, err := factory(http.DefaultClient, rawProviderConfig, core.ProviderOptions{
		DefaultBranch: config.Page.DefaultBranch,
	})
	if err != nil {
		log.Fatalln(err)
	}
	defer provider.Close()
	cacheMeta, err := kv.NewKVFromURL(config.Cache.Meta)
	if err != nil {
		log.Fatalln(err)
	}
	defer cacheMeta.Close()
	cacheBlob, err := cache.NewCacheFromURL(config.Cache.Blob)
	if err != nil {
		log.Fatalln(err)
	}
	defer cacheBlob.Close()
	backend := core.NewProviderCache(provider,
		cacheBlob.Child("backend"),
		uint64(config.Cache.BlobLimit),
		config.Cache.BlobTTL,
		config.Cache.DirTTL,
		config.Cache.BlobConcurrent,
		config.Cache.BackendConcurrent,
		config.Cache.BlobNotFoundTTL,
		config.Cache.DirNotFoundTTL,
	)
	defer backend.Close()
	db, err := kv.NewKVFromURL(config.DB.URL)
	if err != nil {
		log.Fatalln(err)
	}
	defer db.Close()
	userDB := db
	if config.UserDB.URL != "" {
		userDB, err = kv.NewKVFromURL(config.UserDB.URL)
		if err != nil {
			log.Fatalln(err)
		}
		defer userDB.Close()
	}
	event, err := subscribe.NewSubscriberFromURL(config.Event.URL)
	if err != nil {
		log.Fatalln(err)
	}
	defer event.Close()
	fileStorage, err := storage.NewStorageFromURL(config.Storage.URL)
	if err != nil {
		log.Fatalln(err)
	}
	defer fileStorage.Close()
	var authService *core.AuthService
	if config.Auth != nil {
		authProvider, ok := provider.(core.ProviderWithAuth)
		if !ok {
			log.Fatalf("provider %s does not support auth", config.Provider.Type)
		}
		if !authProvider.AuthEnabled() {
			log.Fatalf("provider %s auth is not fully configured", config.Provider.Type)
		}
		authService = core.NewAuthService(authProvider, db.Child("auth"), core.AuthServiceConfig{
			SessionTTL:     config.Auth.SessionTTL,
			StateTTL:       config.Auth.StateTTL,
			AuthzCacheTTL:  config.Auth.AuthzCacheTTL,
			CookieName:     config.Auth.Cookie.Name,
			CookieSecure:   config.Auth.Cookie.Secure,
			CookieDomain:   config.Auth.Cookie.Domain,
			CookieSameSite: parseSameSite(config.Auth.Cookie.SameSite),
			OnUnauthorized: func(w http.ResponseWriter, r *http.Request, err error) {
				config.RenderStatusPage(w, r, http.StatusUnauthorized, err)
			},
			OnForbidden: func(w http.ResponseWriter, r *http.Request, err error) {
				config.RenderStatusPage(w, r, http.StatusForbidden, err)
			},
			OnMethodDenied: func(w http.ResponseWriter, r *http.Request, err error) {
				config.RenderStatusPage(w, r, http.StatusMethodNotAllowed, err)
			},
		})
	}
	if config.Filters == nil {
		config.Filters = make(map[string]map[string]any)
	}
	pageServer, err := pkg.NewPageServer(
		backend,
		config.Domain,
		db,
		userDB,
		pkg.WithClient(http.DefaultClient),
		pkg.WithEvent(event),
		pkg.WithStorage(fileStorage),
		pkg.WithMetaCache(cacheMeta, config.Cache.MetaTTL, config.Cache.MetaRefresh, config.Cache.MetaRefreshConcurrent),
		pkg.WithBlobCache(cacheBlob.Child("filter"), config.Cache.BlobTTL),
		pkg.WithErrorHandler(config.ErrorHandler),
		pkg.WithFilterConfig(config.Filters),
		pkg.WithTrustedProxies(config.TrustedProxies),
		pkg.WithAuth(authService),
	)
	if err != nil {
		log.Fatalln(err)
	}
	slog.Info("server initialized",
		"mode", "server",
		"bind", config.Bind,
		"domain", config.Domain,
		"db", config.DB.URL,
		"user_db", func() string {
			if config.UserDB.URL != "" {
				return config.UserDB.URL
			}
			return config.DB.URL + " (shared)"
		}(),
		"event", config.Event.URL,
		"storage", config.Storage.URL,
		"provider", config.Provider.Type,
	)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	defer stop()

	svc := http.Server{Addr: config.Bind, Handler: pageServer}
	go func() {
		<-ctx.Done()
		slog.Debug("shutdown gracefully")
		_ = svc.Close()
	}()
	_ = svc.ListenAndServe()
}

func parseSameSite(value string) http.SameSite {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	case "default":
		return http.SameSiteDefaultMode
	default:
		return http.SameSiteLaxMode
	}
}

func logInject() {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(utils.NewConsoleLogger(os.Stderr, level))
	slog.Debug("debug enabled")
}
