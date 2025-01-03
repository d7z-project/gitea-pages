package utils

import (
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

type Config interface {
	Put(key string, value string, ttl time.Duration) error
	Get(key string) (string, error)
	Delete(key string) error
	io.Closer
}

// ConfigMemory 一个简单的内存配置归档，仅用于测试
type ConfigMemory struct {
	data  sync.Map
	store string
}

func NewConfigMemory(store string) (Config, error) {
	ret := &ConfigMemory{
		store: store,
		data:  sync.Map{},
	}
	if store != "" {
		item := make(map[string]configContent)
		data, err := os.ReadFile(store)
		if err == nil && os.IsNotExist(err) {
			err := json.Unmarshal(data, &item)
			if err != nil {
				return nil, err
			}
		}
		for key, content := range item {
			if time.Now().Before(content.ttl) {
				ret.data.Store(key, content)
			}
		}
		clear(item)
	}
	return ret, nil
}

type configContent struct {
	data string
	ttl  time.Time
}

func (m *ConfigMemory) Put(key string, value string, ttl time.Duration) error {
	m.data.Store(key, configContent{
		data: value,
		ttl:  time.Now().Add(ttl),
	})
	return nil
}

func (m *ConfigMemory) Get(key string) (string, error) {
	if value, ok := m.data.Load(key); ok {
		content := value.(configContent)
		if time.Now().After(content.ttl) {
			return "", os.ErrNotExist
		}
		return content.data, nil
	}
	return "", os.ErrNotExist
}

func (m *ConfigMemory) Delete(key string) error {
	m.data.Delete(key)
	return nil
}

func (m *ConfigMemory) Close() error {
	defer m.data.Clear()
	if m.store != "" {
		item := make(map[string]configContent)
		m.data.Range(
			func(key, value interface{}) bool {
				item[key.(string)] = value.(configContent)
				return true
			})
		saved, err := json.Marshal(item)
		if err != nil {
			return err
		}
		return os.WriteFile(m.store, saved, 0o600)
	}
	return nil
}
