package core

import (
	"encoding/json"
	"time"

	"github.com/gobwas/glob"
)

type renderCompiler struct {
	regex glob.Glob
	Render
}

// PageConfig 配置

type PageMetaContent struct {
	CommitID     string    `json:"commit-id"`     // 提交 COMMIT ID
	LastModified time.Time `json:"last-modified"` // 上次更新时间
	IsPage       bool      `json:"is-page"`       // 是否为 Page
	ErrorMsg     string    `json:"error"`         // 错误消息

	VRoute  bool                `yaml:"v-route"` // 虚拟路由
	Proxy   map[string]string   `yaml:"proxy"`   // 反向代理
	Renders map[string][]string `json:"renders"` // 配置的渲染器

	Alias  []string `yaml:"aliases"` // 重定向
	Ignore []string `yaml:"ignore"`  // 跳过的内容

	rendersL []*renderCompiler
	ignoreL  []glob.Glob
}

func NewPageMetaContent() *PageMetaContent {
	return &PageMetaContent{
		IsPage:  false,
		Proxy:   make(map[string]string),
		Alias:   make([]string, 0),
		Renders: make(map[string][]string),
		Ignore:  []string{".*", "**/.*"},
	}
}

func (m *PageMetaContent) From(data string) error {
	err := json.Unmarshal([]byte(data), m)
	clear(m.rendersL)
	for key, gs := range m.Renders {
		for _, g := range gs {
			m.rendersL = append(m.rendersL, &renderCompiler{
				regex:  glob.MustCompile(g),
				Render: GetRender(key),
			})
		}
	}
	clear(m.ignoreL)
	for _, g := range m.Ignore {
		m.ignoreL = append(m.ignoreL, glob.MustCompile(g))
	}
	return err
}

func (m *PageMetaContent) IsIgnore(path string) bool {
	for _, g := range m.ignoreL {
		if g.Match(path) {
			return true
		}
	}
	return false
}

func (m *PageMetaContent) TryRender(path ...string) Render {
	for _, s := range path {
		for _, compiler := range m.rendersL {
			if compiler.regex.Match(s) {
				return compiler.Render
			}
		}
	}
	return nil
}

func (m *PageMetaContent) String() string {
	marshal, _ := json.Marshal(m)
	return string(marshal)
}
