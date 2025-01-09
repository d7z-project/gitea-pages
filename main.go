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

	"code.d7z.net/d7z-project/gitea-pages/pkg"
	"code.d7z.net/d7z-project/gitea-pages/pkg/providers"

	"gopkg.in/yaml.v3"
)

var (
	configPath = "config-local.yaml"
	debug      = false
	config     = &Config{}
)

func init() {
	flag.StringVar(&configPath, "conf", configPath, "config file path")
	flag.BoolVar(&debug, "debug", debug, "debug mode")
}

func main() {
	flag.Parse()
	call := logInject()
	defer call()
	loadConf()
	gitea, err := providers.NewGitea(config.Auth.Server, config.Auth.Token)
	if err != nil {
		log.Fatalln(err)
	}
	giteaServer := pkg.NewPageServer(gitea, pkg.DefaultOptions(config.Domain))
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

func loadConf() {
	file, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("read config file failed: %v", err)
	}
	err = yaml.Unmarshal(file, &config)
	if err != nil {
		log.Fatalf("parse config file failed: %v", err)
	}
}
