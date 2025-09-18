package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"gopkg.d7z.net/gitea-pages/pkg"
	"gopkg.d7z.net/gitea-pages/pkg/providers"
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
	call := logInject()
	defer call()
	config, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("fail to load config file: %v", err)
	}
	options, err := config.NewPageServerOptions()
	if err != nil {
		zap.L().Fatal("fail to load options", zap.Error(err))
	}
	gitea, err := providers.NewGitea(config.Auth.Server, config.Auth.Token)
	if err != nil {
		log.Fatalln(err)
	}
	giteaServer := pkg.NewPageServer(gitea, *options)
	defer giteaServer.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	defer stop()

	svc := http.Server{Addr: config.Bind, Handler: giteaServer}
	go func() {
		select {
		case <-ctx.Done():
		}
		zap.L().Debug("shutdown gracefully")
		_ = svc.Close()
	}()
	_ = svc.ListenAndServe()
}

func logInject() func() error {
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
	return logger.Sync
}
