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
	"gopkg.d7z.net/gitea-pages/pkg/utils"
	"gopkg.in/yaml.v3"
)

//go:embed errors.html.tmpl
var defaultErrPage string

type Config struct {
	Bind   string `yaml:"bind"`   // HTTP 绑定
	Domain string `yaml:"domain"` // 基础域名

	Database ConfigDatabase `yaml:"database"` // 配置
	Event    ConfigEvent    `yaml:"event"`    // 事件传递

	Auth ConfigAuth `yaml:"auth"` // 后端认证配置

	Cache ConfigCache `yaml:"cache"` // 缓存配置

	Page ConfigPage `yaml:"page"` // 页面配置

	StaticDir string `yaml:"static"` // 静态资源提供路径

	Filters map[string]map[string]any `yaml:"filters"` // 渲染器配置

	pageErrNotFound, pageErrUnknown *template.Template
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

type ConfigDatabase struct {
	URL string `yaml:"url"`
}
type ConfigEvent struct {
	URL string `yaml:"url"`
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
	var c Config
	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(&c)
	if err != nil {
		return nil, err
	}

	if c.Domain == "" {
		return nil, errors.New("domain is required")
	}
	if c.Database.URL == "" {
		return nil, errors.New("c is required")
	}
	if c.Event.URL == "" {
		c.Event.URL = "memory://"
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
	return &c, nil
}
