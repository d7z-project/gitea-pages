package main

import (
	_ "embed"
	"net/http"
	"os"
	"text/template"
	"time"

	"github.com/alecthomas/units"
	"gopkg.d7z.net/gitea-pages/pkg/middleware/cache"
	"gopkg.d7z.net/gitea-pages/pkg/middleware/config"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gopkg.d7z.net/gitea-pages/pkg"
	"gopkg.d7z.net/gitea-pages/pkg/utils"
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

	Render ConfigRender `yaml:"render"` // 渲染配置
	Proxy  ConfigProxy  `yaml:"proxy"`  // 反向代理配置

	StaticDir string `yaml:"static"` // 静态资源提供路径

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

	if c.StaticDir != "" {
		stat, err := os.Stat(c.StaticDir)
		if err != nil {
			return nil, errors.Wrap(err, "static dir not exists")
		}
		if !stat.IsDir() {
			return nil, errors.New("static dir is not a directory")
		}
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
	defaultErr := utils.MustTemplate(defaultErrPage)
	if c.Page.ErrUnknownPage != "" {
		data, err := os.ReadFile(c.Page.ErrUnknownPage)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read file %s", string(data))
		}
		c.pageErrUnknown = utils.MustTemplate(string(data))
	} else {
		c.pageErrUnknown = defaultErr
	}
	if c.Page.ErrNotFoundPage != "" {
		data, err := os.ReadFile(c.Page.ErrNotFoundPage)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read file %s", c.Page.ErrNotFoundPage)
		}
		c.pageErrNotFound = utils.MustTemplate(string(data))
	} else {
		c.pageErrNotFound = defaultErr
	}

	rel := pkg.ServerOptions{
		Domain:              c.Domain,
		DefaultBranch:       c.Page.DefaultBranch,
		MaxCacheSize:        int(cacheSize),
		HttpClient:          http.DefaultClient,
		MetaTTL:             time.Minute,
		EnableRender:        c.Render.Enable,
		EnableProxy:         c.Proxy.Enable,
		DefaultErrorHandler: c.ErrorHandler,
		StaticDir:           c.StaticDir,
		Cache:               cache.NewCacheMemory(int(cacheMaxSize), int(cacheMaxSize)),
	}
	cfg, err := config.NewAutoConfig(c.Cache.Storage)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to init config memory")
	}
	rel.KVConfig = cfg
	return &rel, nil
}

func (c *Config) ErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, os.ErrNotExist) {
		w.WriteHeader(http.StatusNotFound)
		if err = c.pageErrNotFound.Execute(w, utils.NewTemplateInject(r, map[string]any{
			"UUID":  r.Header.Get("Session-ID"),
			"Error": err,
			"Path":  r.URL.Path,
			"Code":  404,
		})); err != nil {
			zap.L().Error("failed to render error page", zap.Error(err))
		}
	} else {
		w.WriteHeader(http.StatusInternalServerError)
		if err = c.pageErrUnknown.Execute(w, utils.NewTemplateInject(r, map[string]any{
			"UUID":  r.Header.Get("Session-ID"),
			"Error": err,
			"Path":  r.URL.Path,
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

type ConfigProxy struct {
	Enable bool `yaml:"enable"` // 是否允许反向代理
}

type ConfigRender struct {
	Enable bool `yaml:"enable"` // 是否开启渲染器
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
