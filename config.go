package main

import (
	"time"
)

type Config struct {
	Bind string `yaml:"bind"` // HTTP 绑定

	Domain string `yaml:"domain"` // 基础域名

	Auth ConfigAuth `yaml:"auth"` // 后端认证配置

	Cache ConfigCache `yaml:"cache"` // 缓存配置

	Storage string `yaml:"storage"` // 持久化配置
}

type ConfigAuth struct {
	// 服务器地址
	Server string `yaml:"server"`
	// 会话 Id
	Token string `yaml:"token"`
}

type ConfigCache struct {
	ttl    time.Duration `yaml:"ttl"`    // 缓存时间
	length int           `yaml:"length"` // 最大文件大小
}
