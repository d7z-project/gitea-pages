package goja

import (
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

// todo: 新增超时配置

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

			debug := NewDebug(global.EnableDebug && param.Debug && request.URL.Query().
				Get("debug") == "true", request, w)
			program, err := goja.Compile(param.Exec, js, false)
			if err != nil {
				return debug.Flush(err)
			}
			registry := newRegistry(ctx, debug)
			jsLoop := eventloop.NewEventLoop(eventloop.WithRegistry(registry),
				eventloop.EnableConsole(true))

			jsLoop.Start()
			defer jsLoop.Stop()

			closers := NewClosers()
			defer closers.Close()

			stop := make(chan error, 1)
			defer close(stop)

			jsLoop.RunOnLoop(func(vm *goja.Runtime) {
				err := func() error {
					url.Enable(vm)
					if err = RequestInject(ctx, vm, request); err != nil {
						return err
					}
					if err = ResponseInject(vm, debug, request); err != nil {
						return err
					}
					if err = KVInject(ctx, vm); err != nil {
						return err
					}
					if err = EventInject(ctx, vm, jsLoop); err != nil {
						return err
					}
					if global.EnableWebsocket {
						var closer io.Closer
						closer, err = WebsocketInject(ctx, vm, debug, request, jsLoop)
						if err != nil {
							return err
						}
						closers.AddCloser(closer.Close)
					}
					return nil
				}()
				if err != nil {
					stop <- errors.Join(err, errors.New("js init failed"))
					return
				}
				result, err := vm.RunProgram(program)
				if err != nil {
					stop <- err
					return
				}
				export := result.Export()
				if export != nil {
					if promise, ok := export.(*goja.Promise); ok {
						go func() {
							for {
								switch promise.State() {
								case goja.PromiseStateFulfilled:
									stop <- nil
									return
								case goja.PromiseStateRejected:
									switch data := promise.Result().Export().(type) {
									case error:
										stop <- data
									default:
										stop <- errors.New(promise.Result().String())
									}
									return
								default:
									time.Sleep(time.Millisecond * 5)
								}
							}
						}()
					} else {
						stop <- nil
					}
				} else {
					stop <- nil
				}
			})
			resultErr := <-stop
			return debug.Flush(resultErr)
		}, nil
	}, nil
}

func newRegistry(ctx core.FilterContext, printer console.Printer) *require.Registry {
	registry := require.NewRegistry(
		require.WithLoader(func(path string) ([]byte, error) {
			return ctx.PageVFS.Read(ctx, path)
		}),
		require.WithPathResolver(func(base, path string) string {
			return filepath.Join(base, filepath.FromSlash(path))
		}))
	registry.RegisterNativeModule(console.ModuleName, console.RequireWithPrinter(printer))

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
