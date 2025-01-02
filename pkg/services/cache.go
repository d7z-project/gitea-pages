package services

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
)

type Cache interface {
	Put(key string, reader io.Reader) error
	Get(key string) (io.ReadSeekCloser, error)
	Delete(pattern string) error
	io.Closer
}

var ErrCacheOutOfMemory = errors.New("内容无法被缓存，超过最大限定值")

type CacheMemory struct {
	l          sync.RWMutex
	data       map[string][]byte
	sizeGlobal int
	sizeItem   int

	current int
	cache   []byte
	ordered []string
}

func NewCacheMemory(maxUsage, maxGlobalUsage int) *CacheMemory {
	return &CacheMemory{
		data:       make(map[string][]byte),
		l:          sync.RWMutex{},
		sizeGlobal: maxGlobalUsage,
		sizeItem:   maxUsage,

		cache:   make([]byte, maxUsage+1),
		ordered: make([]string, 0),
	}
}

func (c *CacheMemory) Put(key string, reader io.Reader) error {
	c.l.Lock()
	defer c.l.Unlock()
	size, err := io.ReadAtLeast(reader, c.cache, 1)
	if err != nil {
		return err
	}
	if size == len(c.cache) {
		return ErrCacheOutOfMemory
	}
	currentItemSize := len(c.data[key])
	available := c.sizeGlobal + currentItemSize - (c.current + size)
	if available < 0 {
		// 清理旧的内容
		count := 0
		for i, k := range c.ordered {
			available += len(c.data[k])
			if available > 0 {
				break
			}
			count = i + 1
		}

		if available < 0 {
			// 清理全部内容也无法留出空间
			return ErrCacheOutOfMemory
		}
		for _, s := range c.ordered[:count] {
			delete(c.data, s)
		}
		c.ordered = c.ordered[count:]
	}

	dest := make([]byte, size)
	copy(dest, c.cache[:size])
	c.data[key] = dest

	c.current -= currentItemSize
	c.current += len(dest)

	nextOrdered := make([]string, 0, len(c.ordered))
	for _, s := range c.ordered {
		if s != key {
			nextOrdered = append(nextOrdered, s)
		}
	}
	c.ordered = append(nextOrdered, key)
	return nil
}

func (c *CacheMemory) Get(key string) (io.ReadSeekCloser, error) {
	c.l.RLock()
	defer c.l.RUnlock()
	if i, ok := c.data[key]; ok {
		return nopCloser{
			bytes.NewReader(i),
		}, nil
	}
	return nil, os.ErrNotExist
}

func (c *CacheMemory) Delete(pattern string) error {
	c.l.Lock()
	defer c.l.Unlock()
	nextOrder := make([]string, 0, len(c.ordered))
	for _, key := range c.ordered {
		if strings.HasPrefix(key, pattern) {
			c.current -= len(c.data[key])
			delete(c.data, key)
		} else {
			nextOrder = append(nextOrder, key)
		}
	}
	clear(c.ordered)
	c.ordered = nextOrder
	return nil
}

func (c *CacheMemory) Close() error {
	c.l.Lock()
	defer c.l.Unlock()
	clear(c.ordered)
	clear(c.data)
	c.current = 0
	return nil
}

type nopCloser struct {
	io.ReadSeeker
}

func (nopCloser) Close() error { return nil }
