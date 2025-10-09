package config

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
)

// ConfigEtcd 实现了KVConfig接口的etcd配置存储
type ConfigEtcd struct {
	client *clientv3.Client
	lease  clientv3.Lease
}

// NewConfigEtcd 创建一个新的etcd配置存储实例
// endpoints是etcd集群的地址列表，如["localhost:2379"]
// tlsConfig是TLS配置，如果为nil则使用无认证模式
func NewConfigEtcd(endpoints []string, tlsConfig *tls.Config) (*ConfigEtcd, error) {
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("endpoints is empty")
	}

	zap.L().Debug("connect etcd", zap.Strings("endpoints", endpoints))

	// 创建etcd客户端配置
	cfg := clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
		TLS:         tlsConfig,
	}

	// 建立连接
	client, err := clientv3.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %v", err)
	}

	// 创建lease用于处理TTL
	lease := clientv3.NewLease(client)

	return &ConfigEtcd{
		client: client,
		lease:  lease,
	}, nil
}

// Put 存储键值对，可以设置过期时间
func (e *ConfigEtcd) Put(ctx context.Context, key string, value string, ttl time.Duration) error {
	if ttl == TtlKeep || ttl <= 0 {
		// 无过期时间，直接存储
		_, err := e.client.Put(ctx, key, value)
		return err
	}

	// 创建lease
	resp, err := e.lease.Grant(ctx, int64(ttl.Seconds()))
	if err != nil {
		return fmt.Errorf("failed to grant lease: %v", err)
	}

	// 关联lease存储键值对
	_, err = e.client.Put(ctx, key, value, clientv3.WithLease(resp.ID))
	return err
}

// Get 获取键对应的值
func (e *ConfigEtcd) Get(ctx context.Context, key string) (string, error) {
	resp, err := e.client.Get(ctx, key)
	if err != nil {
		return "", fmt.Errorf("failed to get key: %v", err)
	}

	if len(resp.Kvs) == 0 {
		return "", os.ErrNotExist
	}

	return string(resp.Kvs[0].Value), nil
}

// Delete 删除指定的键
func (e *ConfigEtcd) Delete(ctx context.Context, key string) error {
	_, err := e.client.Delete(ctx, key)
	return err
}

// Close 关闭etcd客户端连接
func (e *ConfigEtcd) Close() error {
	if err := e.lease.Close(); err != nil {
		zap.L().Warn("failed to close etcd lease", zap.Error(err))
	}
	return e.client.Close()
}
