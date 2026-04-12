package goja

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	nurl "net/url"
	"slices"
	"strings"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
)

func installFetch(ctx context.Context, vm *goja.Runtime, loop *eventloop.EventLoop, client *http.Client, cfg FetchConfig) error {
	return vm.Set("fetch", func(resource goja.Value, init ...goja.Value) *goja.Promise {
		promise, resolve, reject := vm.NewPromise()

		go func() {
			if !cfg.Enabled {
				loop.RunOnLoop(func(*goja.Runtime) {
					_ = reject(errors.New("fetch is disabled"))
				})
				return
			}

			requestURL := resource.String()
			method := http.MethodGet
			headers := make(http.Header)
			var body io.Reader

			if len(init) > 0 && !goja.IsUndefined(init[0]) && !goja.IsNull(init[0]) {
				initObj := init[0].ToObject(vm)
				if initObj != nil {
					if value := initObj.Get("method"); !goja.IsUndefined(value) && !goja.IsNull(value) {
						method = strings.ToUpper(value.String())
					}
					if value := initObj.Get("headers"); !goja.IsUndefined(value) && !goja.IsNull(value) {
						headers = headersFromValue(vm, value)
					}
					if value := initObj.Get("body"); !goja.IsUndefined(value) && !goja.IsNull(value) {
						payload, err := bodyBytesFromValue(vm, value)
						if err != nil {
							loop.RunOnLoop(func(*goja.Runtime) {
								_ = reject(err)
							})
							return
						}
						body = strings.NewReader(string(payload))
					}
				}
			}

			if err := validateFetchTarget(requestURL, cfg); err != nil {
				loop.RunOnLoop(func(*goja.Runtime) {
					_ = reject(err)
				})
				return
			}

			req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
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

			loop.RunOnLoop(func(runtime *goja.Runtime) {
				_ = resolve(newResponseObject(runtime, &webResponseState{
					status:     resp.StatusCode,
					statusText: resp.Status,
					headers:    cloneHeaderValues(resp.Header),
					body:       respBody,
				}))
			})
		}()

		return promise
	})
}

func validateFetchTarget(rawURL string, cfg FetchConfig) error {
	parsed, err := nurl.Parse(rawURL)
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
