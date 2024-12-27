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
		data: make(map[string]string),
		lock: sync.RWMutex{},
	}
}

// ConfigMemory 一个简单的内存配置归档，仅用于测试
type ConfigMemory struct {
	data map[string]string
	lock sync.RWMutex
}

func (m *ConfigMemory) Put(key string, value string) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.data[key] = value
	return nil
}

func (m *ConfigMemory) Get(key string) (string, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	v, ok := m.data[key]
	if !ok {
		return "", os.ErrNotExist
	}
	return v, nil
}

func (m *ConfigMemory) Delete(key string) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	delete(m.data, key)
	return nil
}

func (m *ConfigMemory) Close() error {
	m.lock.Lock()
	defer m.lock.Unlock()
	clear(m.data)
	return nil
}
