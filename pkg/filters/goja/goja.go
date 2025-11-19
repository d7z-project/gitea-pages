package goja

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/console"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/dop251/goja_nodejs/require"
	"github.com/dop251/goja_nodejs/url"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func FilterInstGoJa(gl core.Params) (core.FilterInstance, error) {
	var global struct {
		Timeout         time.Duration `json:"timeout"`
		EnableDebug     bool          `json:"debug"`
		EnableWebsocket bool          `json:"websocket"`
	}
	global.EnableDebug = true
	global.EnableWebsocket = true
	if err := gl.Unmarshal(&global); err != nil {
		return nil, err
	}
	if global.Timeout == 0 {
		global.Timeout = 60 * time.Second
	}
	return func(config core.Params) (core.FilterCall, error) {
		var param struct {
			Exec  string `json:"exec"`
			Debug bool   `json:"debug"`
		}
		if err := config.Unmarshal(&param); err != nil {
			return nil, err
		}
		if param.Exec == "" {
			return nil, errors.New("no exec specified")
		}
		return func(ctx core.FilterContext, w http.ResponseWriter, request *http.Request, next core.NextCall) error {
			js, err := ctx.ReadString(ctx, param.Exec)
			if err != nil {
				return err
			}
			prg, err := goja.Compile("main.js", js, false)
			if err != nil {
				return err
			}
			debug := NewDebug(global.EnableDebug && param.Debug && request.URL.Query().Get("debug") == "true", request, w)
			registry := newRegistry(ctx)
			registry.RegisterNativeModule(console.ModuleName, console.RequireWithPrinter(debug))
			loop := eventloop.NewEventLoop(eventloop.WithRegistry(registry), eventloop.EnableConsole(true))
			stop := make(chan struct{}, 1)
			shutdown := make(chan struct{}, 1)
			defer close(shutdown)
			timeout, timeoutCancelFunc := context.WithTimeout(ctx, global.Timeout)
			defer timeoutCancelFunc()
			count := 0
			closers := NewClosers()
			defer closers.Close()
			go func() {
				defer func() {
					shutdown <- struct{}{}
				}()
				select {
				case <-timeout.Done():
				case <-stop:
				}
				count = loop.Stop()
			}()
			loop.Run(func(vm *goja.Runtime) {
				url.Enable(vm)
				if err = RequestInject(ctx, vm, request); err != nil {
					panic(err)
				}
				if err = ResponseInject(vm, debug, request); err != nil {
					panic(err)
				}
				if err = KVInject(ctx, vm); err != nil {
					panic(err)
				}
				if err = EventInject(ctx, vm); err != nil {
					panic(err)
				}
				if global.EnableWebsocket {
					var closer io.Closer
					closer, err = WebsocketInject(ctx, vm, debug, request, timeoutCancelFunc)
					if err != nil {
						panic(err)
					}
					closers.AddCloser(closer.Close)
				}
				_, err = vm.RunProgram(prg)
			})
			stop <- struct{}{}
			close(stop)
			<-shutdown
			if count != 0 {
				err = errors.Join(context.DeadlineExceeded, err)
			}
			return debug.Flush(err)
		}, nil
	}, nil
}

func newRegistry(ctx core.FilterContext) *require.Registry {
	registry := require.NewRegistry(
		require.WithLoader(func(path string) ([]byte, error) {
			return ctx.PageVFS.Read(ctx, path)
		}),
		require.WithPathResolver(func(base, path string) string {
			return filepath.Join(base, filepath.FromSlash(path))
		}))
	return registry
}

type Closers struct {
	mu      sync.Mutex
	closers []func() error
}

func NewClosers() *Closers {
	return &Closers{
		closers: make([]func() error, 0),
	}
}

func (c *Closers) AddCloser(closer func() error) {
	c.mu.Lock()
	c.closers = append(c.closers, closer)
	c.mu.Unlock()
}

func (c *Closers) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var errs []error
	for i := len(c.closers) - 1; i >= 0; i-- {
		if err := c.closers[i](); err != nil {
			errs = append(errs, err)
		}
	}
	c.closers = nil
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
