package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	"gopkg.d7z.net/gitea-pages/pkg"
	"gopkg.d7z.net/gitea-pages/pkg/providers"
	"gopkg.d7z.net/gitea-pages/pkg/utils"
	"gopkg.d7z.net/middleware/cache"
	"gopkg.d7z.net/middleware/kv"
	"gopkg.d7z.net/middleware/subscribe"
	"gopkg.in/yaml.v3"
)

var (
	org    = "pub"
	domain = "fbi.com"
	repo   = org + "." + domain
	path   = ""

	port = ":8080"
)

func init() {
	slog.SetDefault(utils.NewConsoleLogger(os.Stderr, slog.LevelDebug))
	dir, _ := os.Getwd()
	path = dir
	flag.StringVar(&org, "org", org, "org")
	flag.StringVar(&repo, "repo", repo, "repo")
	flag.StringVar(&domain, "domain", domain, "domain")
	flag.StringVar(&path, "path", path, "path")
	flag.StringVar(&port, "port", port, "port")
	flag.Parse()
}

func main() {
	if stat, err := os.Stat(path); err != nil || !stat.IsDir() {
		slog.Error("path is not a directory", "path", path, "error", err)
		os.Exit(1)
	}
	provider := providers.NewLocalProvider(map[string][]string{
		org: {repo},
	}, path)

	file, _ := os.ReadFile(filepath.Join(path, ".pages.yaml"))
	if file != nil {
		var info map[string]interface{}
		err := yaml.Unmarshal(file, &info)
		if err != nil {
			slog.Error("parse yaml", "error", err)
			os.Exit(1)
		}
		delete(info, "alias")
		marshal, _ := yaml.Marshal(info)
		provider.AddOverlay(".pages.yaml", marshal)
	}
	db, err := kv.NewMemory("")
	if err != nil {
		slog.Error("failed to init memory provider", "error", err)
		os.Exit(1)
	}
	userDB, err := kv.NewMemory("")
	if err != nil {
		slog.Error("failed to init user memory provider", "error", err)
		os.Exit(1)
	}
	subscriber := subscribe.NewMemorySubscriber()
	server, err := pkg.NewPageServer(
		provider, domain, db, userDB,
		pkg.WithClient(http.DefaultClient),
		pkg.WithEvent(subscriber),
		pkg.WithMetaCache(db, 0, 0, 0),
		pkg.WithBlobCache(&nopCache{}, 0),
		pkg.WithErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, os.ErrNotExist) {
				http.Error(w, "page not found.", http.StatusNotFound)
			} else if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}),
		pkg.WithFilterConfig(map[string]map[string]any{
			"redirect": {
				"scheme": "http",
			},
		}),
	)
	if err != nil {
		slog.Error("failed to init page", "error", err)
		os.Exit(1)
	}
	slog.Info("server initialized",
		"mode", "local",
		"listen", port,
		"domain", domain,
		"org", org,
		"repo", repo,
		"path", path,
		"url", "http://"+repo+port+"/",
	)
	err = http.ListenAndServe(port, server)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("failed to start server", "error", err)
		os.Exit(1)
	}
}

type nopCache struct{}

func (n *nopCache) Child(_ ...string) cache.Cache {
	return n
}

func (n *nopCache) Put(_ context.Context, _ string, _ map[string]string, _ io.Reader, _ time.Duration) error {
	return nil
}

func (n *nopCache) Get(_ context.Context, _ string) (*cache.Content, error) {
	return nil, os.ErrNotExist
}

func (n *nopCache) Delete(_ context.Context, _ string) error {
	return nil
}
