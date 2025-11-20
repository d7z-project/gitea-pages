package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gopkg.d7z.net/gitea-pages/pkg"
	"gopkg.d7z.net/gitea-pages/pkg/providers"
	"gopkg.d7z.net/middleware/cache"
	"gopkg.d7z.net/middleware/kv"
	"gopkg.d7z.net/middleware/subscribe"
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
	zap.L().Info("exec workdir", zap.String("path", path))
	flag.StringVar(&org, "org", org, "org")
	flag.StringVar(&repo, "repo", repo, "repo")
	flag.StringVar(&domain, "domain", domain, "domain")
	flag.StringVar(&path, "path", path, "path")
	flag.StringVar(&port, "port", port, "port")
	flag.Parse()
}

func main() {
	fmt.Printf("请访问 http://%s%s/", repo, port)
	if stat, err := os.Stat(path); err != nil || !stat.IsDir() {
		zap.L().Fatal("path is not a directory", zap.String("path", path))
	}
	provider := providers.NewLocalProvider(map[string][]string{
		org: {repo},
	}, path)
	memory, err := kv.NewMemory("")
	if err != nil {
		zap.L().Fatal("failed to init memory provider", zap.Error(err))
	}
	subscriber := subscribe.NewMemorySubscriber()
	server, err := pkg.NewPageServer(http.DefaultClient,
		provider, domain, "gh-pages", memory, subscriber, memory, 0, &nopCache{}, 0,
		func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, os.ErrNotExist) {
				http.Error(w, "page not found.", http.StatusNotFound)
			} else if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}, make(map[string]map[string]any))
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
