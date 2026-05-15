package goja

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	nurl "net/url"
	"slices"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
)

type lookupNetIPFunc func(context.Context, string, string) ([]netip.Addr, error)
type dialContextFunc func(context.Context, string, string) (net.Conn, error)

func newFetchClient(cfg FetchConfig) *http.Client {
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: newFetchTransport(cfg),
	}
}

func newFetchTransport(cfg FetchConfig) *http.Transport {
	base, _ := http.DefaultTransport.(*http.Transport)
	transport := base.Clone()
	if cfg.BlockPrivateNetwork {
		transport.Proxy = func(*http.Request) (*nurl.URL, error) { return nil, nil }
	}
	dial := transport.DialContext
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	transport.DialContext = restrictedDialContext(cfg, net.DefaultResolver.LookupNetIP, dial)
	return transport
}

func restrictedDialContext(cfg FetchConfig, lookup lookupNetIPFunc, dial dialContextFunc) dialContextFunc {
	if !cfg.BlockPrivateNetwork {
		return dial
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := lookup(ctx, "ip", host)
		if err != nil {
			return nil, err
		}

		var lastDialErr error
		hasAllowed := false
		for _, ip := range ips {
			ip = ip.Unmap()
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
				continue
			}
			hasAllowed = true
			target := net.JoinHostPort(ip.String(), port)
			conn, err := dial(ctx, network, target)
			if err == nil {
				return conn, nil
			}
			lastDialErr = err
		}
		if !hasAllowed {
			return nil, errors.New("fetch target ip is not allowed")
		}
		if lastDialErr != nil {
			return nil, lastDialErr
		}
		return nil, errors.New("fetch target ip is not allowed")
	}
}

func installFetch(ctx context.Context, vm *goja.Runtime, loop *eventloop.EventLoop, client *http.Client, cfg FetchConfig, runtime *runtimeState) error {
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
		if !runtime.startTask() {
			_ = reject(errRuntimeClosing)
			return promise
		}

		body := io.Reader(nil)
		if len(requestState.body) > 0 {
			body = bytes.NewReader(requestState.body)
		}
		headers := cloneHeaderValues(requestState.headers)

		go func() {
			defer runtime.finishTask()
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
				runtime.runOnLoop(loop, func(*goja.Runtime) {
					_ = reject(err)
				})
				return
			}
			req.Header = headers

			resp, err := client.Do(req)
			if err != nil {
				runtime.runOnLoop(loop, func(*goja.Runtime) {
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
				runtime.runOnLoop(loop, func(*goja.Runtime) {
					_ = reject(err)
				})
				return
			}
			if cfg.MaxResponseBodyBytes > 0 && int64(len(respBody)) > cfg.MaxResponseBodyBytes {
				runtime.runOnLoop(loop, func(*goja.Runtime) {
					_ = reject(errors.New("fetch response body exceeds limit"))
				})
				return
			}

			runtime.runOnLoop(loop, func(vm *goja.Runtime) {
				_ = resolve(newResponseObject(vm, &webResponseState{
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
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("fetch scheme is not allowed")
	}
	host := parsed.Hostname()
	if host == "" {
		return errors.New("missing fetch host")
	}
	if len(cfg.AllowedHosts) > 0 && !slices.Contains(cfg.AllowedHosts, host) {
		return errors.New("fetch target host is not allowed")
	}
	return nil
}
