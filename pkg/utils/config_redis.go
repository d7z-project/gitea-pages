package utils

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/redis/go-redis/v9"
)

type ConfigRedis struct {
	ctx    context.Context
	client *redis.Client
}

func NewConfigRedis(ctx context.Context, addr string, password string, db int) (*ConfigRedis, error) {
	if addr == "" {
		return nil, fmt.Errorf("addr is empty")
	}
	zap.L().Debug("connect redis", zap.String("addr", addr), zap.Int("db", db))
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	_, err := client.Ping(ctx).Result()
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	return &ConfigRedis{
		ctx:    ctx,
		client: client,
	}, nil
}

func (r *ConfigRedis) Put(key string, value string, ttl time.Duration) error {
	return r.client.Set(r.ctx, key, value, ttl).Err()
}

func (r *ConfigRedis) Get(key string) (string, error) {
	return r.client.Get(r.ctx, key).Result()
}

func (r *ConfigRedis) Delete(key string) error {
	return r.client.Del(r.ctx, key).Err()
}

func (r *ConfigRedis) Close() error {
	return r.client.Close()
}
