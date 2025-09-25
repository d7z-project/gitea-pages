package cache

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.d7z.net/gitea-pages/pkg/utils"
)

func NewCacheMemory(maxUsage, maxGlobalUsage int) *CacheMemory {
	return &CacheMemory{
		data:       make(map[string]*[]byte),
		lastModify: make(map[string]time.Time),
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
	size := 0
	// 可以指定空的 reader 作为 404 缓存
	if reader != nil {
		var err error
		size, err = io.ReadAtLeast(reader, c.cache, 1)
		if err != nil {
			return err
		}
	}
	if size == len(c.cache) {
		return ErrCacheOutOfMemory
	}
	currentItemSize := 0
	if data, ok := c.data[key]; ok {
		currentItemSize = len(*data)
	}
	available := c.sizeGlobal + currentItemSize - (c.current + size)
	if available < 0 {
		// 清理旧的内容
		count := 0
		for i, k := range c.ordered {
			available += len(*c.data[k])
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
			delete(c.lastModify, s)
		}
		c.ordered = c.ordered[count:]
	}

	if reader != nil {
		dest := make([]byte, size)
		copy(dest, c.cache[:size])
		c.data[key] = &dest
		c.lastModify[key] = time.Now()

		c.current -= currentItemSize
		c.current += len(dest)
	} else {
		c.data[key] = nil
		c.lastModify[key] = time.Now()

		c.current -= currentItemSize
	}

	nextOrdered := make([]string, 0, len(c.ordered))
	for _, s := range c.ordered {
		if s != key {
			nextOrdered = append(nextOrdered, s)
		}
	}
	c.ordered = append(nextOrdered, key)
	return nil
}

func (c *CacheMemory) Get(key string) (*CacheContent, error) {
	c.l.RLock()
	defer c.l.RUnlock()
	if i, ok := c.data[key]; ok {
		if i == nil {
			return nil, nil
		}

		return &CacheContent{
			ReadSeekCloser: utils.NopCloser{
				bytes.NewReader(*i),
			},
			Length:       len(*i),
			LastModified: c.lastModify[key],
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
			c.current -= len(*c.data[key])
			delete(c.data, key)
			delete(c.lastModify, key)
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
	clear(c.lastModify)
	c.current = 0
	return nil
}
