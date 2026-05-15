package core

import (
	"strings"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

type PageConfig struct {
	Alias    []string          `yaml:"alias"`   // 重定向地址
	Routes   []PageConfigRoute `yaml:"routes"`  // 路由配置
	Private  bool              `yaml:"private"` // 是否私有
	Security PageSecurity      `yaml:"security"`
}

type PageConfigRoute struct {
	Path   string         `yaml:"path"`
	Type   string         `yaml:"type"`
	Params map[string]any `yaml:"params"`
}

func (p *PageConfigRoute) UnmarshalYAML(value *yaml.Node) error {
	p.Params = make(map[string]any)
	if value == nil || value.Kind != yaml.MappingNode {
		return errors.New("route must be a mapping")
	}

	var (
		pathFound  bool
		filterNode *yaml.Node
	)

	for i := 0; i+1 < len(value.Content); i += 2 {
		key := strings.TrimSpace(value.Content[i].Value)
		node := value.Content[i+1]
		if key == "" {
			return errors.New("route key cannot be empty")
		}
		if key == "path" {
			if pathFound {
				return errors.New("duplicate path field")
			}
			if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
				return errors.New("route path must be a string")
			}
			if strings.TrimSpace(node.Value) == "" {
				return errors.New("route path cannot be empty")
			}
			p.Path = node.Value
			pathFound = true
			continue
		}
		if p.Type != "" {
			return errors.Errorf("route must define exactly one filter, got %q and %q", p.Type, key)
		}
		p.Type = key
		filterNode = node
	}

	if !pathFound {
		return errors.New("missing path field")
	}
	if p.Type == "" {
		return errors.New("missing filter field")
	}
	if filterNode == nil {
		return nil
	}

	if filterNode.Kind == yaml.ScalarNode {
		if filterNode.Tag == "!!null" || filterNode.Tag == "!!str" {
			return nil
		}
		return errors.Errorf("route filter %q must be a mapping, string or null", p.Type)
	}
	return filterNode.Decode(&p.Params)
}
