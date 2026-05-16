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

type (
	lookupNetIPFunc func(context.Context, string, string) ([]netip.Addr, error)
	dialContextFunc func(context.Context, string, string) (net.Conn, error)
)

var (
	errFetchDisabled                 = errors.New("fetch is disabled")
	errFetchAborted                  = errors.New("fetch aborted")
	errFetchResponseBodyExceedsLimit = errors.New("fetch response body exceeds limit")
	errFetchTargetIPNotAllowed       = errors.New("fetch target ip is not allowed")
)

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
			return nil, errFetchTargetIPNotAllowed
		}
		if lastDialErr != nil {
			return nil, lastDialErr
		}
		return nil, errFetchTargetIPNotAllowed
	}
}

func installFetch(ctx context.Context, vm *goja.Runtime, loop *eventloop.EventLoop, client *http.Client, cfg FetchConfig, runtime *runtimeState, closers *Closers) error {
	return vm.Set("fetch", func(resource goja.Value, init ...goja.Value) *goja.Promise {
		if !cfg.Enabled {
			return rejectedPromise(vm, errFetchDisabled)
		}

		requestState, err := requestStateFromInput(vm, resource)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		if len(init) > 0 && !isNilish(init[0]) {
			if err := applyRequestInit(vm, requestState, init[0]); err != nil {
				return rejectedPromise(vm, err)
			}
		}
		if requestState.abort != nil && requestState.abort.Aborted() {
			return rejectedPromise(vm, errFetchAborted)
		}
		if err := validateFetchTarget(requestState.url, cfg); err != nil {
			return rejectedPromise(vm, err)
		}
		headers := cloneHeaderValues(requestState.headers)
		return asyncValuePromise(vm, loop, runtime, func() (*http.Response, error) {
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

			var body io.Reader
			if requestState.body != nil && !requestState.body.empty() {
				payload, err := consumeWebBody(newBodyState(requestState.headers, &requestState.used, requestState.body, nil, runtime))
				if err != nil {
					return nil, err
				}
				if len(payload) > 0 {
					body = bytes.NewReader(payload)
				}
			}

			req, err := http.NewRequestWithContext(reqCtx, requestState.method, requestState.url, body)
			if err != nil {
				return nil, err
			}
			req.Header = headers

			resp, err := client.Do(req)
			if err != nil {
				if requestState.abort != nil && requestState.abort.Aborted() {
					return nil, errFetchAborted
				}
				return nil, err
			}
			if cfg.MaxResponseBodyBytes > 0 && resp.ContentLength > cfg.MaxResponseBodyBytes {
				_ = resp.Body.Close()
				return nil, errFetchResponseBodyExceedsLimit
			}
			closers.AddCloser(resp.Body.Close)
			return resp, nil
		}, func(vm *goja.Runtime, resp *http.Response) (goja.Value, error) {
			var (
				read int64
				used bool
			)
			bodySource := newStreamingBodySource(func() (io.ReadCloser, error) {
				if used {
					return nil, errors.New("body stream already read")
				}
				used = true
				if cfg.MaxResponseBodyBytes <= 0 {
					return resp.Body, nil
				}
				return newLimitedReadCloser(resp.Body, func(n int) error {
					read += int64(n)
					if read > cfg.MaxResponseBodyBytes {
						return errFetchResponseBodyExceedsLimit
					}
					return nil
				}), nil
			})
			return newResponseObject(vm, loop, runtime, &webResponseState{
				status:     resp.StatusCode,
				statusText: http.StatusText(resp.StatusCode),
				headers:    cloneHeaderValues(resp.Header),
				body:       bodySource,
			}), nil
		})
	})
}

type limitedReadCloser struct {
	reader io.ReadCloser
	onRead func(int) error
}

func newLimitedReadCloser(reader io.ReadCloser, onRead func(int) error) io.ReadCloser {
	return &limitedReadCloser{reader: reader, onRead: onRead}
}

func (l *limitedReadCloser) Read(p []byte) (int, error) {
	n, err := l.reader.Read(p)
	if n > 0 && l.onRead != nil {
		if limitErr := l.onRead(n); limitErr != nil {
			_ = l.reader.Close()
			return n, limitErr
		}
	}
	return n, err
}

func (l *limitedReadCloser) Close() error {
	return l.reader.Close()
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
