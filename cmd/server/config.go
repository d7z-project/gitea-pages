package main

import (
	_ "embed"
	"encoding/json"
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

	DB             ConfigDatabase `yaml:"db"`       // 程序内部使用的存储
	UserDB         ConfigDatabase `yaml:"user_db"`  // 用户脚本使用的存储
	LegacyDatabase ConfigDatabase `yaml:"database"` // 兼容旧配置

	Event ConfigEvent `yaml:"event"` // 事件传递

	Provider ConfigProvider `yaml:"provider"` // 内容 Provider 配置

	Auth *ConfigAuth `yaml:"auth"` // 页面认证配置，可选

	Cache ConfigCache `yaml:"cache"` // 缓存配置

	Page ConfigPage `yaml:"page"` // 页面配置

	StaticDir string `yaml:"static"` // 静态资源提供路径

	Filters map[string]map[string]any `yaml:"filters"` // 渲染器配置

	pageErrUnauthorized, pageErrForbidden, pageErrNotFound, pageErrMethodDenied, pageErrUnknown *template.Template
}

type ConfigProvider struct {
	Type      string `yaml:"type"`
	providers map[string]json.RawMessage
}

func (c *Config) ErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, os.ErrNotExist) {
		status = http.StatusNotFound
	}
	c.RenderStatusPage(w, r, status, err)
}

func (c *Config) RenderStatusPage(w http.ResponseWriter, r *http.Request, status int, err error) {
	page := c.statusTemplate(status)
	if page == nil {
		http.Error(w, http.StatusText(status), status)
		return
	}
	w.WriteHeader(status)
	if renderErr := page.Execute(w, utils.NewTemplateInject(r, map[string]any{
		"UUID":  r.Header.Get("Session-ID"),
		"Error": err,
		"Path":  r.URL.Path,
		"Code":  status,
	})); renderErr != nil {
		zap.L().Error("failed to render error page", zap.Error(renderErr))
	}
}

func (c *Config) statusTemplate(status int) *template.Template {
	switch status {
	case http.StatusUnauthorized:
		return c.pageErrUnauthorized
	case http.StatusForbidden:
		return c.pageErrForbidden
	case http.StatusNotFound:
		return c.pageErrNotFound
	case http.StatusMethodNotAllowed:
		return c.pageErrMethodDenied
	default:
		return c.pageErrUnknown
	}
}

type ConfigAuth struct {
	SessionTTL    time.Duration    `yaml:"session_ttl"`
	StateTTL      time.Duration    `yaml:"state_ttl"`
	AuthzCacheTTL time.Duration    `yaml:"authz_cache_ttl"`
	Cookie        ConfigAuthCookie `yaml:"cookie"`
}

type ConfigAuthCookie struct {
	Name     string `yaml:"name"`
	Secure   bool   `yaml:"secure"`
	Domain   string `yaml:"domain"`
	SameSite string `yaml:"same_site"`
}

type ConfigPage struct {
	DefaultBranch   string `yaml:"default_branch"`
	ErrUnauthorized string `yaml:"401"`
	ErrForbidden    string `yaml:"403"`
	ErrNotFoundPage string `yaml:"404"`
	ErrMethodDenied string `yaml:"405"`
	ErrUnknownPage  string `yaml:"500"`
}

type ConfigDatabase struct {
	URL string `yaml:"url"`
}
type ConfigEvent struct {
	URL string `yaml:"url"`
}

type ConfigCache struct {
	Meta                  string        `yaml:"meta"`                    // 元数据缓存
	MetaTTL               time.Duration `yaml:"meta_ttl"`                // 缓存时间
	MetaRefresh           time.Duration `yaml:"meta_refresh"`            // 刷新时间
	MetaRefreshConcurrent int           `yaml:"meta_refresh_concurrent"` // 并发刷新限制

	Blob              string           `yaml:"blob"`               // 缓存归档位置
	BlobTTL           time.Duration    `yaml:"blob_ttl"`           // 缓存归档位置
	BlobLimit         units.Base2Bytes `yaml:"blob_limit"`         // 单个文件最大大小
	DirTTL            time.Duration    `yaml:"dir_ttl"`            // 目录列表缓存时间
	BlobConcurrent    uint64           `yaml:"blob_concurrent"`    // 并发缓存限制
	BlobNotFoundTTL   time.Duration    `yaml:"blob_not_found_ttl"` // 404 缓存时间
	DirNotFoundTTL    time.Duration    `yaml:"dir_not_found_ttl"`  // 目录 404 缓存时间
	BackendConcurrent uint64           `yaml:"backend_concurrent"` // 并发后端请求限制
}

func (c ConfigProvider) ProviderConfig(name string) (json.RawMessage, bool) {
	if c.providers == nil {
		return nil, false
	}
	value, ok := c.providers[name]
	return value, ok
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err = yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	c.Provider.providers, err = loadProviderConfigs(data, "provider", map[string]struct{}{
		"type": {},
	})
	if err != nil {
		return nil, err
	}

	if c.DB.URL == "" {
		c.DB = c.LegacyDatabase
		if c.DB.URL != "" {
			zap.L().Warn("config key 'database' is deprecated; use 'db' and optional 'user_db' instead")
		}
	}
	if c.DB.URL == "" {
		return nil, errors.New("db.url is required")
	}
	if c.Provider.Type == "" {
		return nil, errors.New("provider.type is required")
	}
	if _, ok := c.Provider.ProviderConfig(c.Provider.Type); !ok {
		return nil, errors.Errorf("provider.%s config is required", c.Provider.Type)
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
	c.pageErrUnauthorized, err = loadErrorTemplate(c.Page.ErrUnauthorized, defaultErr)
	if err != nil {
		return nil, err
	}
	c.pageErrForbidden, err = loadErrorTemplate(c.Page.ErrForbidden, defaultErr)
	if err != nil {
		return nil, err
	}
	c.pageErrNotFound, err = loadErrorTemplate(c.Page.ErrNotFoundPage, defaultErr)
	if err != nil {
		return nil, err
	}
	c.pageErrMethodDenied, err = loadErrorTemplate(c.Page.ErrMethodDenied, defaultErr)
	if err != nil {
		return nil, err
	}
	c.pageErrUnknown, err = loadErrorTemplate(c.Page.ErrUnknownPage, defaultErr)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func loadErrorTemplate(path string, fallback *template.Template) (*template.Template, error) {
	if path == "" {
		return fallback, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read file %s", path)
	}
	return utils.MustTemplate(string(data)), nil
}

func loadProviderConfigs(data []byte, section string, commonKeys map[string]struct{}) (map[string]json.RawMessage, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 {
		return nil, nil
	}
	sectionNode := findMappingValue(root.Content[0], section)
	if sectionNode == nil || sectionNode.Kind != yaml.MappingNode {
		return nil, nil
	}
	configs := make(map[string]json.RawMessage)
	for i := 0; i < len(sectionNode.Content); i += 2 {
		key := sectionNode.Content[i].Value
		if _, ok := commonKeys[key]; ok {
			continue
		}
		value, err := yamlNodeToJSON(sectionNode.Content[i+1])
		if err != nil {
			return nil, err
		}
		configs[key] = value
	}
	return configs, nil
}

func findMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func yamlNodeToJSON(node *yaml.Node) (json.RawMessage, error) {
	var value any
	if err := node.Decode(&value); err != nil {
		return nil, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return data, nil
}
