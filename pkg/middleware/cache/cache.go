package cache

import (
	"io"
	"sync"
	"time"

	"github.com/pkg/errors"
)

type CacheContent struct {
	io.ReadSeekCloser
	Length       int
	LastModified time.Time
}

func (c *CacheContent) ReadToString() (string, error) {
	all, err := io.ReadAll(c)
	if err != nil {
		return "", err
	}
	return string(all), nil
}

type Cache interface {
	Put(key string, reader io.Reader) error
	// Get return CacheContent or nil when put nil io.reader
	Get(key string) (*CacheContent, error)
	Delete(pattern string) error
	io.Closer
}

var ErrCacheOutOfMemory = errors.New("内容无法被缓存，超过最大限定值")

// TODO: 优化锁结构
// 复杂场景请使用其他缓存服务

type CacheMemory struct {
	l          sync.RWMutex
	data       map[string]*[]byte
	lastModify map[string]time.Time
	sizeGlobal int
	sizeItem   int

	current int
	cache   []byte
	ordered []string
}
