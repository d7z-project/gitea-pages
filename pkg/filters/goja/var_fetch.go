package goja

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
)

func FetchInject(ctx context.Context, jsCtx *goja.Runtime, loop *eventloop.EventLoop, client *http.Client, cfg FetchConfig) error {
	return jsCtx.GlobalObject().Set("fetch", func(url string, options ...map[string]interface{}) *goja.Promise {
		promise, resolve, reject := jsCtx.NewPromise()

		go func() {
			if !cfg.Enabled {
				loop.RunOnLoop(func(*goja.Runtime) {
					_ = reject(errors.New("fetch is disabled"))
				})
				return
			}

			method := "GET"
			var body io.Reader
			headers := make(http.Header)

			if len(options) > 0 {
				opts := options[0]
				if m, ok := opts["method"].(string); ok {
					method = strings.ToUpper(m)
				}
				if h, ok := opts["headers"].(map[string]interface{}); ok {
					for k, v := range h {
						if strVal, ok := v.(string); ok {
							headers.Set(k, strVal)
						}
					}
				}
				if b, ok := opts["body"].(string); ok {
					body = strings.NewReader(b)
				}
			}

			if err := validateFetchTarget(url, cfg); err != nil {
				loop.RunOnLoop(func(*goja.Runtime) {
					_ = reject(err)
				})
				return
			}

			req, err := http.NewRequestWithContext(ctx, method, url, body)
			if err != nil {
				loop.RunOnLoop(func(*goja.Runtime) {
					_ = reject(err)
				})
				return
			}
			req.Header = headers

			resp, err := client.Do(req)
			if err != nil {
				loop.RunOnLoop(func(*goja.Runtime) {
					_ = reject(err)
				})
				return
			}
			defer resp.Body.Close()

			reader := io.Reader(resp.Body)
			if cfg.MaxResponseBodyBytes > 0 {
				reader = io.LimitReader(resp.Body, cfg.MaxResponseBodyBytes+1)
			}
			respBody, err := io.ReadAll(reader)
			if err != nil {
				loop.RunOnLoop(func(*goja.Runtime) {
					_ = reject(err)
				})
				return
			}
			if cfg.MaxResponseBodyBytes > 0 && int64(len(respBody)) > cfg.MaxResponseBodyBytes {
				loop.RunOnLoop(func(*goja.Runtime) {
					_ = reject(errors.New("fetch response body exceeds limit"))
				})
				return
			}

			headersMap := make(map[string]interface{})
			for k, v := range resp.Header {
				headersMap[k] = v
			}

			loop.RunOnLoop(func(vm *goja.Runtime) {
				responseObj := map[string]interface{}{
					"ok":         resp.StatusCode >= 200 && resp.StatusCode < 300,
					"status":     resp.StatusCode,
					"statusText": resp.Status,
					"headers":    headersMap,
					"text": func() *goja.Promise {
						p, res, _ := vm.NewPromise()
						_ = res(string(respBody))
						return p
					},
					"json": func() *goja.Promise {
						p, res, rej := vm.NewPromise()
						var data interface{}
						if err := json.Unmarshal(respBody, &data); err != nil {
							_ = rej(err)
						} else {
							_ = res(data)
						}
						return p
					},
				}
				_ = resolve(responseObj)
			})
		}()

		return promise
	})
}

func validateFetchTarget(rawURL string, cfg FetchConfig) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	host := parsed.Hostname()
	if host == "" {
		return errors.New("missing fetch host")
	}
	if len(cfg.AllowedHosts) > 0 && !slices.Contains(cfg.AllowedHosts, host) {
		return errors.New("fetch target host is not allowed")
	}
	if cfg.BlockPrivateNetwork {
		if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()) {
			return errors.New("fetch target ip is not allowed")
		}
	}
	return nil
}
