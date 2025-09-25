package config

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
	Put(ctx context.Context, key string, value string, ttl time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, key string) error
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
		pass := query.Get("pass")
		if pass == "" {
			pass = query.Get("password")
		}
		db := strings.TrimPrefix(parse.Path, "/")
		if db == "" {
			db = "0"
		}
		dbi, err := strconv.Atoi(db)
		if err != nil {
			return nil, err
		}
		return NewConfigRedis(parse.Host, pass, dbi)
	default:
		return nil, fmt.Errorf("unsupported scheme: %s", parse.Scheme)
	}
}
