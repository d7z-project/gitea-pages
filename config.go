package main

import (
	_ "embed"
	"net/http"
	"os"
	"text/template"
	"time"

	"github.com/alecthomas/units"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gopkg.d7z.net/gitea-pages/pkg"
	"gopkg.d7z.net/gitea-pages/pkg/utils"
	"gopkg.d7z.net/middleware/cache"
	"gopkg.d7z.net/middleware/kv"
	"gopkg.in/yaml.v3"
)

//go:embed errors.html.tmpl
var defaultErrPage string

type Config struct {
	Bind   string `yaml:"bind"`   // HTTP 绑定
	Domain string `yaml:"domain"` // 基础域名

	Config string `yaml:"config"` // 配置

	Auth ConfigAuth `yaml:"auth"` // 后端认证配置

	Cache ConfigCache `yaml:"cache"` // 缓存配置

	Page ConfigPage `yaml:"page"` // 页面配置

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

	if c.Config == "" {
		return nil, errors.New("config is required")
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

	memoryCache, err := cache.NewMemoryCache(cache.MemoryCacheConfig{
		MaxCapacity: 8102,
		CleanupInt:  time.Hour,
	})
	if err != nil {
		return nil, errors.Wrap(err, "create cache")
	}
	alias, err := kv.NewKVFromURL(c.Config)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to init alias config")
	}
	cacheMeta, err := kv.NewKVFromURL(c.Cache.Meta)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to init cache meta")
	}
	rel := pkg.ServerOptions{
		Domain:              c.Domain,
		DefaultBranch:       c.Page.DefaultBranch,
		Alias:               alias,
		CacheMeta:           cacheMeta,
		CacheMetaTTL:        c.Cache.MetaTTL,
		CacheBlob:           memoryCache,
		CacheBlobTTL:        c.Cache.BlobTTL,
		CacheBlobLimit:      uint64(c.Cache.BlobLimit),
		HttpClient:          http.DefaultClient,
		EnableRender:        c.Render.Enable,
		EnableProxy:         c.Proxy.Enable,
		StaticDir:           c.StaticDir,
		DefaultErrorHandler: c.ErrorHandler,
	}
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
	Meta    string        `yaml:"meta"`     // 元数据缓存
	MetaTTL time.Duration `yaml:"meta_ttl"` // 缓存时间

	Blob      string           `yaml:"blob"`       // 缓存归档位置
	BlobTTL   time.Duration    `yaml:"blob_ttl"`   // 缓存归档位置
	BlobLimit units.Base2Bytes `yaml:"blob_limit"` // 单个文件最大大小
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
