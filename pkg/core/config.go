package core

import "strings"

type PageConfig struct {
	Alias  []string          `yaml:"alias"`     // 重定向地址
	Render map[string]string `yaml:"templates"` // 渲染器地址

	VirtualRoute bool              `yaml:"v-route"` // 是否使用虚拟路由（任何路径均使用 /index.html 返回 200 响应）
	ReverseProxy map[string]string `yaml:"proxy"`   // 反向代理路由

	Ignore string `yaml:"ignore"` // 跳过展示的内容
}

func (p *PageConfig) Ignores() []string {
	i := make([]string, 0)
	if p.Ignore == "" {
		return i
	}
	for _, line := range strings.Split(p.Ignore, "\n") {
		for _, item := range strings.Split(line, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			i = append(i, item)
		}
	}
	return i
}

func (p *PageConfig) Renders() map[string]string {
	result := make(map[string]string)
	for sType, patterns := range p.Render {
		for _, line := range strings.Split(patterns, "\n") {
			for _, item := range strings.Split(line, ",") {
				item = strings.TrimSpace(item)
				if item == "" {
					continue
				}
				result[sType] = item
			}
		}
	}
	return result
}
