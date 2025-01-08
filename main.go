package main

import (
	"flag"
	"log"
	"net/http"
	"os"

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
	inject := debugInject()
	defer inject()
	loadConf()
	gitea, err := providers.NewGitea(config.Auth.Server, config.Auth.Token)
	if err != nil {
		log.Fatalln(err)
	}
	server := pkg.NewPageServer(gitea, pkg.DefaultOptions(config.Domain))
	mux := http.NewServeMux()
	mux.Handle("/", server)
	defer server.Close()
	_ = http.ListenAndServe(config.Bind, mux)
}

func debugInject() func() error {
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
