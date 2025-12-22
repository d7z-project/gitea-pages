package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
	"gopkg.d7z.net/gitea-pages/pkg"
	"gopkg.d7z.net/gitea-pages/pkg/providers"
	"gopkg.d7z.net/middleware/cache"
	"gopkg.d7z.net/middleware/kv"
	"gopkg.d7z.net/middleware/subscribe"
)

var (
	configPath = "config-local.yaml"
	debug      = false
)

func init() {
	flag.StringVar(&configPath, "conf", configPath, "config file path")
	flag.BoolVar(&debug, "debug", debug, "debug mode")
	flag.Parse()
}

func main() {
	call := logInject()
	defer call()
	config, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("fail to load config file: %v", err)
	}

	gitea, err := providers.NewGitea(http.DefaultClient, config.Auth.Server, config.Auth.Token, config.Page.DefaultBranch)
	if err != nil {
		log.Fatalln(err)
	}
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
	backend := providers.NewProviderCache(gitea,
		cacheBlob.Child("backend"), uint64(config.Cache.BlobLimit),
	)
	defer backend.Close()
	db, err := kv.NewKVFromURL(config.Database.URL)
	if err != nil {
		log.Fatalln(err)
	}
	defer db.Close()
	cdb, ok := db.(kv.RawKV).Raw().(kv.CursorPagedKV)
	if !ok {
		log.Fatalln(errors.New("database not support cursor"))
	}
	event, err := subscribe.NewSubscriberFromURL(config.Event.URL)
	if err != nil {
		log.Fatalln(err)
	}
	defer event.Close()
	if config.Filters == nil {
		config.Filters = make(map[string]map[string]any)
	}
	pageServer, err := pkg.NewPageServer(
		http.DefaultClient,
		backend,
		config.Domain,
		cdb,
		event,
		cacheMeta,
		config.Cache.MetaTTL,
		cacheBlob.Child("filter"),
		config.Cache.BlobTTL,
		config.ErrorHandler,
		config.Filters,
	)
	if err != nil {
		log.Fatalln(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	defer stop()

	svc := http.Server{Addr: config.Bind, Handler: pageServer}
	go func() {
		<-ctx.Done()
		zap.L().Debug("shutdown gracefully")
		_ = svc.Close()
	}()
	_ = svc.ListenAndServe()
}

func logInject() func() {
	atom := zap.NewAtomicLevel()
	if debug {
		atom.SetLevel(zap.DebugLevel)
	} else {
		atom.SetLevel(zap.InfoLevel)
	}
	cfg := zap.NewProductionConfig()
	cfg.Level = atom

	logger, _ := cfg.Build()
	zap.ReplaceGlobals(logger)
	zap.L().Debug("debug enabled")
	return func() {
		if err := logger.Sync(); err != nil {
			fmt.Println(err)
		}
	}
}
