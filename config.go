package main

import (
	_ "embed"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"time"

	sprig "github.com/go-task/slim-sprig/v3"

	"github.com/alecthomas/units"

	"code.d7z.net/d7z-project/gitea-pages/pkg"
	"code.d7z.net/d7z-project/gitea-pages/pkg/utils"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

//go:embed errors.html.tmpl
var defaultErrPage string

type Config struct {
	Bind   string `yaml:"bind"`   // HTTP 绑定
	Domain string `yaml:"domain"` // 基础域名

	Auth  ConfigAuth  `yaml:"auth"`  // 后端认证配置
	Cache ConfigCache `yaml:"cache"` // 缓存配置
	Page  ConfigPage  `yaml:"page"`  // 页面配置

	pageErrNotFound, pageErrUnknown *template.Template
}

func (c *Config) NewPageServerOptions() (*pkg.ServerOptions, error) {
	if c.Domain == "" {
		return nil, errors.New("domain is required")
	}
	var err error
	var cacheSize, cacheMaxSize units.Base2Bytes
	cacheSize, err = units.ParseBase2Bytes(c.Cache.FileSize)
	if err != nil {
		return nil, errors.Wrap(err, "parse cache size")
	}

	cacheMaxSize, err = units.ParseBase2Bytes(c.Cache.MaxSize)
	if err != nil {
		return nil, errors.Wrap(err, "parse cache max size")
	}
	if cacheMaxSize <= cacheSize {
		return nil, errors.New("cache max size must be greater than or equal to file max size")
	}
	if c.Page.DefaultBranch == "" {
		c.Page.DefaultBranch = "gh-pages"
	}
	defaultErr := template.Must(template.New("err").Funcs(sprig.FuncMap()).Parse(defaultErrPage))
	if c.Page.ErrUnknownPage != "" {
		data, err := os.ReadFile(c.Page.ErrUnknownPage)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read file %s", string(data))
		}
		c.pageErrUnknown = template.Must(template.New("err").Funcs(sprig.FuncMap()).Parse(c.Page.ErrUnknownPage))
	} else {
		c.pageErrUnknown = defaultErr
	}
	if c.Page.ErrNotFoundPage != "" {
		data, err := os.ReadFile(c.Page.ErrNotFoundPage)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read file %s", c.Page.ErrNotFoundPage)
		}
		c.pageErrNotFound = template.Must(template.New("err").Funcs(sprig.FuncMap()).Parse(string(data)))
	} else {
		c.pageErrNotFound = defaultErr
	}

	rel := pkg.ServerOptions{
		Domain:              c.Domain,
		DefaultBranch:       c.Page.DefaultBranch,
		MaxCacheSize:        int(cacheSize),
		HttpClient:          http.DefaultClient,
		DefaultErrorHandler: c.ErrorHandler,
		Cache:               utils.NewCacheMemory(int(cacheMaxSize), int(cacheMaxSize)),
	}
	if c.Cache.Storage != "" {
		if err := os.MkdirAll(filepath.Dir(c.Cache.Storage), 0o755); err != nil && !os.IsExist(err) {
			return nil, err
		}
	}
	memory, err := utils.NewConfigMemory(c.Cache.Storage)
	if err != nil {
		return nil, err
	}
	rel.Config = memory
	return &rel, nil
}

func (c *Config) ErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, os.ErrNotExist) {
		w.WriteHeader(http.StatusNotFound)
		if err = c.pageErrNotFound.Execute(w, utils.NewTemplateInject(r, map[string]any{
			"Error": err,
			"Code":  404,
		})); err != nil {
			zap.L().Error("failed to render error page", zap.Error(err))
		}
	} else {
		w.WriteHeader(http.StatusInternalServerError)
		if err = c.pageErrUnknown.Execute(w, utils.NewTemplateInject(r, map[string]any{
			"Error": err,
			"Code":  500,
		})); err != nil {
			zap.L().Error("failed to render error page", zap.Error(err))
		}
	}
}

type ConfigAuth struct {
	// 服务器地址
	Server string `yaml:"server"`
	// 会话 Id
	Token string `yaml:"token"`
}

type ConfigPage struct {
	DefaultBranch   string `yaml:"default_branch"`
	ErrNotFoundPage string `yaml:"404"`
	ErrUnknownPage  string `yaml:"500"`
}

type ConfigCache struct {
	Storage string `yaml:"storage"` // 缓存归档位置

	Ttl      time.Duration `yaml:"ttl"`  // 缓存时间
	FileSize string        `yaml:"size"` // 单个文件最大大小
	MaxSize  string        `yaml:"max"`  // 最大文件大小
}

func LoadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var config Config
	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(&config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}
