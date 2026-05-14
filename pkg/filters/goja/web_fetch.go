package goja

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	nurl "net/url"
	"slices"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
)

func installFetch(ctx context.Context, vm *goja.Runtime, loop *eventloop.EventLoop, client *http.Client, cfg FetchConfig) error {
	return vm.Set("fetch", func(resource goja.Value, init ...goja.Value) *goja.Promise {
		promise, resolve, reject := vm.NewPromise()
		if !cfg.Enabled {
			_ = reject(errors.New("fetch is disabled"))
			return promise
		}

		requestState := requestStateFromInput(vm, resource)
		if len(init) > 0 && !isNilish(init[0]) {
			if err := applyRequestInit(vm, requestState, init[0]); err != nil {
				_ = reject(err)
				return promise
			}
		}
		if requestState.abort != nil && requestState.abort.Aborted() {
			_ = reject(errors.New("fetch aborted"))
			return promise
		}
		if err := validateFetchTarget(requestState.url, cfg); err != nil {
			_ = reject(err)
			return promise
		}

		body := io.Reader(nil)
		if len(requestState.body) > 0 {
			body = bytes.NewReader(requestState.body)
		}
		headers := cloneHeaderValues(requestState.headers)

		go func() {
			reqCtx := ctx
			if requestState.abort != nil {
				var cancel context.CancelFunc
				reqCtx, cancel = context.WithCancel(ctx)
				defer cancel()
				go func() {
					select {
					case <-requestState.abort.Done():
						cancel()
					case <-reqCtx.Done():
					}
				}()
			}

			req, err := http.NewRequestWithContext(reqCtx, requestState.method, requestState.url, body)
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
					if requestState.abort != nil && requestState.abort.Aborted() {
						_ = reject(errors.New("fetch aborted"))
						return
					}
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
					statusText: http.StatusText(resp.StatusCode),
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
