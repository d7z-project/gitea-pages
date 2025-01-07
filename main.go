package main

import (
	"flag"
	"log"
	"log/slog"
	"net/http"
	"os"

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
	debugInject()
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

func debugInject() {
	programLevel := new(slog.LevelVar)
	programLevel.Set(slog.LevelDebug)
	if debug {
		h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: programLevel})
		slog.SetDefault(slog.New(h))
	}
	slog.Debug("debug mode")
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
