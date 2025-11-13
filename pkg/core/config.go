package core

import (
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

type PageConfig struct {
	Alias  []string          `yaml:"alias"`  // 重定向地址
	Routes []PageConfigRoute `yaml:"routes"` // 路由配置
}

type PageConfigRoute struct {
	Path   string         `yaml:"path"`
	Type   string         `yaml:"type"`
	Params map[string]any `yaml:"params"`
}

func (p *PageConfigRoute) UnmarshalYAML(value *yaml.Node) error {
	var data map[string]any
	if err := value.Decode(&data); err != nil {
		return err
	}
	if item, ok := data["path"]; ok {
		p.Path = item.(string)
	} else {
		return errors.New("missing path field")
	}
	delete(data, "path")
	keys := make([]string, 0)
	for k := range data {
		keys = append(keys, k)
	}
	if len(keys) != 1 {
		return errors.New("invalid param")
	}
	p.Type = keys[0]
	params := data[p.Type]
	// 跳过空参数
	p.Params = make(map[string]any)
	if _, ok := params.(string); ok || params == nil {
		return nil
	}
	out, err := yaml.Marshal(params)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(out, &p.Params)
}
