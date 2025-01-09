package utils

import (
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
)

const TtlKeep = -1

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
			if content.Ttl == nil || time.Now().Before(*content.Ttl) {
				ret.data.Store(key, content)
			}
		}
		clear(item)
	}
	return ret, nil
}

type configContent struct {
	Data string     `json:"data"`
	Ttl  *time.Time `json:"ttl,omitempty"`
}

func (m *ConfigMemory) Put(key string, value string, ttl time.Duration) error {
	d := time.Now().Add(ttl)
	td := &d
	if ttl == -1 {
		td = nil
	}
	m.data.Store(key, configContent{
		Data: value,
		Ttl:  td,
	})
	return nil
}

func (m *ConfigMemory) Get(key string) (string, error) {
	if value, ok := m.data.Load(key); ok {
		content := value.(configContent)
		if content.Ttl != nil && time.Now().After(*content.Ttl) {
			return "", os.ErrNotExist
		}
		return content.Data, nil
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
		now := time.Now()
		m.data.Range(
			func(key, value interface{}) bool {
				content := value.(configContent)
				if content.Ttl == nil || now.Before(*content.Ttl) {
					item[key.(string)] = content
				}
				return true
			})
		zap.L().Debug("回写内容到本地存储", zap.String("store", m.store), zap.Int("length", len(item)))
		saved, err := json.Marshal(item)
		if err != nil {
			return err
		}
		return os.WriteFile(m.store, saved, 0o600)
	}
	return nil
}
