package services

import (
	"io"
	"os"
	"sync"
)

type Config interface {
	Put(key string, value string) error
	Get(key string) (string, error)
	Delete(key string) error
	io.Closer
}

func NewConfigMemory() Config {
	return &ConfigMemory{
		data: sync.Map{},
	}
}

// ConfigMemory 一个简单的内存配置归档，仅用于测试
type ConfigMemory struct {
	data sync.Map
}

func (m *ConfigMemory) Put(key string, value string) error {
	m.data.Store(key, value)
	return nil
}

func (m *ConfigMemory) Get(key string) (string, error) {
	if value, ok := m.data.Load(key); ok {
		return value.(string), nil
	}
	return "", os.ErrNotExist
}

func (m *ConfigMemory) Delete(key string) error {
	m.data.Delete(key)
	return nil
}

func (m *ConfigMemory) Close() error {
	m.data.Clear()
	return nil
}
