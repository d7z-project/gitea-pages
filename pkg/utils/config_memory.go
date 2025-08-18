package utils

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ConfigMemory 一个简单的内存配置归档，仅用于测试
type ConfigMemory struct {
	data  sync.Map
	store string
}

func NewConfigMemory(store string) (KVConfig, error) {
	ret := &ConfigMemory{
		store: store,
		data:  sync.Map{},
	}
	if store != "" {
		zap.L().Info("parse config from store", zap.String("store", store))
		if err := os.MkdirAll(filepath.Dir(store), 0o755); err != nil && !os.IsExist(err) {
			return nil, err
		}
		item := make(map[string]ConfigContent)
		data, err := os.ReadFile(store)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err == nil {
			err = json.Unmarshal(data, &item)
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

type ConfigContent struct {
	Data string     `json:"data"`
	Ttl  *time.Time `json:"ttl,omitempty"`
}

func (m *ConfigMemory) Put(ctx context.Context, key string, value string, ttl time.Duration) error {
	d := time.Now().Add(ttl)
	td := &d
	if ttl == -1 {
		td = nil
	}
	m.data.Store(key, ConfigContent{
		Data: value,
		Ttl:  td,
	})
	return nil
}

func (m *ConfigMemory) Get(ctx context.Context, key string) (string, error) {
	if value, ok := m.data.Load(key); ok {
		content := value.(ConfigContent)
		if content.Ttl != nil && time.Now().After(*content.Ttl) {
			return "", os.ErrNotExist
		}
		return content.Data, nil
	}
	return "", os.ErrNotExist
}

func (m *ConfigMemory) Delete(ctx context.Context, key string) error {
	m.data.Delete(key)
	return nil
}

func (m *ConfigMemory) Close() error {
	defer m.data.Clear()
	if m.store != "" {
		item := make(map[string]ConfigContent)
		now := time.Now()
		m.data.Range(
			func(key, value interface{}) bool {
				content := value.(ConfigContent)
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
