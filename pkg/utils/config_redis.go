package utils

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/pkg/errors"
	"github.com/valkey-io/valkey-go"

	"go.uber.org/zap"
)

type ConfigRedis struct {
	ctx    context.Context
	client valkey.Client
}

func NewConfigRedis(ctx context.Context, addr string, password string, db int) (*ConfigRedis, error) {
	if addr == "" {
		return nil, fmt.Errorf("addr is empty")
	}
	zap.L().Debug("connect redis", zap.String("addr", addr))
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{addr},
		Password:    password,
		SelectDB:    db,
	})
	if err != nil {
		return nil, err
	}
	return &ConfigRedis{
		ctx:    ctx,
		client: client,
	}, nil
}

func (r *ConfigRedis) Put(key string, value string, ttl time.Duration) error {
	builder := r.client.B().Set().Key(key).Value(value)
	if ttl != TtlKeep {
		builder.Ex(ttl)
	}
	return r.client.Do(r.ctx, builder.Nx().Build()).Error()
}

func (r *ConfigRedis) Get(key string) (string, error) {
	v, err := r.client.Do(r.ctx, r.client.B().Get().Key(key).Build()).ToString()
	if err != nil && errors.Is(err, valkey.Nil) {
		return "", os.ErrNotExist
	}
	return v, err
}

func (r *ConfigRedis) Delete(key string) error {
	return r.client.Do(r.ctx, r.client.B().Decr().Key(key).Build()).Error()
}

func (r *ConfigRedis) Close() error {
	r.client.Close()
	return nil
}
