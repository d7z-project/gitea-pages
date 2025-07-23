package utils

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const TtlKeep = -1

type KVConfig interface {
	Put(key string, value string, ttl time.Duration) error
	Get(key string) (string, error)
	Delete(key string) error
	io.Closer
}

func NewAutoConfig(src string) (KVConfig, error) {
	if src == "" ||
		strings.HasPrefix(src, "./") ||
		strings.HasPrefix(src, "/") ||
		strings.HasPrefix(src, "\\") ||
		strings.HasPrefix(src, ".\\") {
		return NewConfigMemory(src)
	}
	parse, err := url.Parse(src)
	if err != nil {
		return nil, err
	}
	switch parse.Scheme {
	case "local":
		return NewConfigMemory(parse.Path)
	case "redis":
		query := parse.Query()
		addr := query.Get("addr")
		pass := query.Get("pass")
		db := query.Get("db")
		dbi, err := strconv.Atoi(db)
		if err != nil {
			return nil, err
		}
		return NewConfigRedis(context.Background(), addr, pass, dbi)
	default:
		return nil, fmt.Errorf("unsupported scheme: %s", parse.Scheme)
	}
}
