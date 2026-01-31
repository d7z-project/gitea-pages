package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gopkg.d7z.net/gitea-pages/pkg"
	"gopkg.d7z.net/gitea-pages/pkg/providers"
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
	atom := zap.NewAtomicLevel()
	atom.SetLevel(zap.DebugLevel)
	cfg := zap.NewProductionConfig()
	cfg.Level = atom
	logger, _ := cfg.Build()
	zap.ReplaceGlobals(logger)
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
	fmt.Printf("请访问 http://%s%s/ ,本地路径: %s\n", repo, port, path)
	if stat, err := os.Stat(path); err != nil || !stat.IsDir() {
		zap.L().Fatal("path is not a directory", zap.String("path", path))
	}
	provider := providers.NewLocalProvider(map[string][]string{
		org: {repo},
	}, path)

	file, _ := os.ReadFile(filepath.Join(path, ".pages.yaml"))
	if file != nil {
		var info map[string]interface{}
		err := yaml.Unmarshal(file, &info)
		if err != nil {
			zap.L().Fatal("parse yaml", zap.Error(err))
		}
		delete(info, "alias")
		marshal, _ := yaml.Marshal(info)
		provider.AddOverlay(".pages.yaml", marshal)
	}
	memory, err := kv.NewMemory("")
	if err != nil {
		zap.L().Fatal("failed to init memory provider", zap.Error(err))
	}
	subscriber := subscribe.NewMemorySubscriber()
	server, err := pkg.NewPageServer(
		provider, domain, memory,
		pkg.WithClient(http.DefaultClient),
		pkg.WithEvent(subscriber),
		pkg.WithMetaCache(memory, 0, 0),
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
		zap.L().Fatal("failed to init page", zap.Error(err))
	}
	err = http.ListenAndServe(port, server)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		zap.L().Fatal("failed to start server", zap.Error(err))
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
