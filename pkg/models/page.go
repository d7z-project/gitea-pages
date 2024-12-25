package models

type PageConfig struct {
	// pages 分支
	Branch string `json:"branch" yaml:"branch"`
	// 匹配的域名和路径
	Domain string `json:"domain" yaml:"domain"`
	// 路由模式 (default / history)
	RouteMode string `json:"route" yaml:"route"`
}
