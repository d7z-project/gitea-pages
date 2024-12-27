package services

import (
	"errors"
	"io"
	"sync"
	"time"
)

type Cache interface {
	Put(key string, reader io.Reader) error
	Get(key string) (io.ReadSeekCloser, error)
	Delete(pattern string) error
	io.Closer
}

var ErrCacheOutOfMemory = errors.New("内容无法被缓存，超过最大限定值")

type CacheMemory struct {
	l       sync.RWMutex
	data    map[string][]byte
	maxAge  time.Duration
	sizeGl  int
	sizeOne int

	current int
	cache   []byte
	ordered []string
}

func NewCacheMemory(maxUsage, maxGlobalUsage int) *CacheMemory {
	return &CacheMemory{
		data:    make(map[string][]byte),
		l:       sync.RWMutex{},
		sizeGl:  maxGlobalUsage,
		sizeOne: maxUsage,

		cache:   make([]byte, maxUsage+1),
		ordered: make([]string, 0),
	}
}

func (c *CacheMemory) Put(key string, reader io.Reader) error {
	c.l.Lock()
	defer c.l.Unlock()
	size, err := io.ReadAtLeast(reader, c.cache, 0)
	if err != nil {
		return err
	}
	if size == len(c.cache) {
		return ErrCacheOutOfMemory
	}
	needed := c.sizeGl - c.current + size
	if needed < 0 {
		// 清理旧的内容
		count := 0
		for i, k := range c.ordered {
			needed += len(c.data[k])
			if needed > 0 {
				break
			}
			count = i + 1
		}

		if needed < 0 {
			// 清理全部内容也无法留出空间
			return ErrCacheOutOfMemory
		}
		for _, s := range c.ordered[:count] {
			delete(c.data, s)
			c.current -= len(c.data)
		}
		c.ordered = c.ordered[count:]
	}

	dest := make([]byte, size)
	copy(dest, c.cache[:size])
	c.data[key] = dest
	c.ordered = append(c.ordered, key)
	c.current += len(dest)
	return nil
}

func (c *CacheMemory) Get(key string) (io.ReadSeekCloser, error) {
	//TODO implement me
	panic("implement me")
}

func (c *CacheMemory) Delete(key string) error {
	//TODO implement me
	panic("implement me")
}

func (c *CacheMemory) Close() error {
	//TODO implement me
	panic("implement me")
}
