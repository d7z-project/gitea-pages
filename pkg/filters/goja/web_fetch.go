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
	"time"

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

			requestState, err := fetchRequestState(vm, resource, init)
			if err != nil {
				loop.RunOnLoop(func(*goja.Runtime) {
					_ = reject(err)
				})
				return
			}
			if isAbortedSignal(requestState.signal) {
				loop.RunOnLoop(func(*goja.Runtime) {
					_ = reject(errors.New("fetch aborted"))
				})
				return
			}

			if err = validateFetchTarget(requestState.url, cfg); err != nil {
				loop.RunOnLoop(func(*goja.Runtime) {
					_ = reject(err)
				})
				return
			}

			reqCtx := ctx
			if requestState.signal != nil {
				var cancel context.CancelFunc
				reqCtx, cancel = context.WithCancel(ctx)
				defer cancel()
				go func() {
					ticker := time.NewTicker(10 * time.Millisecond)
					defer ticker.Stop()
					for reqCtx.Err() == nil {
						if isAbortedSignal(requestState.signal) {
							cancel()
							return
						}
						<-ticker.C
					}
				}()
			}

			req, err := http.NewRequestWithContext(reqCtx, requestState.method, requestState.url, bytesReader(requestState.body))
			if err != nil {
				loop.RunOnLoop(func(*goja.Runtime) {
					_ = reject(err)
				})
				return
			}
			req.Header = cloneHeaderValues(requestState.headers)

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
					statusText: http.StatusText(resp.StatusCode),
					headers:    cloneHeaderValues(resp.Header),
					body:       respBody,
				}))
			})
		}()

		return promise
	})
}

func fetchRequestState(vm *goja.Runtime, resource goja.Value, init []goja.Value) (*webRequestState, error) {
	state := requestStateFromInput(vm, resource)
	if len(init) == 0 || isNilish(init[0]) {
		return state, nil
	}
	if err := applyRequestInit(vm, state, init[0]); err != nil {
		return nil, err
	}
	return state, nil
}

func isAbortedSignal(signal *goja.Object) bool {
	if signal == nil {
		return false
	}
	value, ok := objectValue(signal, "aborted")
	if !ok {
		return false
	}
	if aborted, ok := value.Export().(bool); ok {
		return aborted
	}
	return false
}

func bytesReader(body []byte) io.Reader {
	if len(body) == 0 {
		return nil
	}
	return bytes.NewReader(body)
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
