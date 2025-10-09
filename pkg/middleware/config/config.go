package config

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/url"
	"os"
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
	case "etcd":
		query := parse.Query()
		endpoints := []string{parse.Host}

		// 检查是否有多个端点
		if endpointsStr := query.Get("endpoints"); endpointsStr != "" {
			endpoints = strings.Split(endpointsStr, ",")
		}

		// 检查是否需要TLS认证
		var tlsConfig *tls.Config
		caFile := query.Get("ca-file")
		certFile := query.Get("cert-file")
		keyFile := query.Get("key-file")

		if caFile != "" && certFile != "" && keyFile != "" {
			caCert, err := os.ReadFile(caFile)
			if err != nil {
				return nil, fmt.Errorf("failed to read ca file: %v", err)
			}

			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("failed to add ca certificate")
			}

			cert, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err != nil {
				return nil, fmt.Errorf("failed to load client certificate: %v", err)
			}

			tlsConfig = &tls.Config{
				Certificates: []tls.Certificate{cert},
				RootCAs:      caCertPool,
			}
		} else if caFile != "" || certFile != "" || keyFile != "" {
			// 部分TLS参数被提供，视为错误
			return nil, fmt.Errorf("incomplete tls configuration, need ca-file, cert-file and key-file")
		}

		return NewConfigEtcd(endpoints, tlsConfig)
	default:
		return nil, fmt.Errorf("unsupported scheme: %s", parse.Scheme)
	}
}
