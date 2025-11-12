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
	"gopkg.in/yaml.v3"

	"gopkg.d7z.net/gitea-pages/pkg"
	"gopkg.d7z.net/gitea-pages/pkg/providers"
	_ "gopkg.d7z.net/gitea-pages/pkg/renders"
)

var (
	configPath = "config-local.yaml"
	debug      = false
	generate   = false
)

func init() {
	flag.StringVar(&configPath, "conf", configPath, "config file path")
	flag.BoolVar(&generate, "generate", debug, "generate config file")
	flag.BoolVar(&debug, "debug", debug, "debug mode")
	flag.Parse()
}

func main() {
	if generate {
		var cfg Config
		file, err := os.ReadFile(configPath)
		if err == nil {
			_ = yaml.Unmarshal(file, &cfg)
		}
		out, err := yaml.Marshal(&cfg)
		if err != nil {
			log.Fatal("marshal config file failed", zap.Error(err))
		}
		err = os.WriteFile(configPath, out, 0o644)
		if err != nil {
			log.Fatal("write config file failed", zap.Error(err))
		}
		return
	}

	call := logInject()
	defer func() {
		_ = call()
	}()
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
	backend := providers.NewProviderCache(gitea, options.CacheMeta, options.CacheMetaTTL,
		options.CacheBlob, options.CacheBlobLimit,
	)
	giteaServer := pkg.NewPageServer(backend, *options)
	defer giteaServer.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	defer stop()

	svc := http.Server{Addr: config.Bind, Handler: giteaServer}
	go func() {
		<-ctx.Done()
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
