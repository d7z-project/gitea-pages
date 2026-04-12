package goja

import (
	"bytes"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/buffer"
	"github.com/dop251/goja_nodejs/console"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/dop251/goja_nodejs/require"
	"github.com/dop251/goja_nodejs/url"
	lru "github.com/hashicorp/golang-lru/v2"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

var programCache *lru.Cache[string, *goja.Program]

type FetchConfig struct {
	Enabled              bool     `json:"enabled"`
	MaxResponseBodyBytes int64    `json:"max_response_body_bytes"`
	AllowedHosts         []string `json:"allowed_hosts"`
	BlockPrivateNetwork  bool     `json:"block_private_network"`
}

type RequestConfig struct {
	MaxBodyBytes int64 `json:"max_body_bytes"`
}

type FSConfig struct {
	Enabled bool `json:"enabled"`
}

type Config struct {
	EnableDebug     bool          `json:"debug"`
	EnableWebsocket bool          `json:"websocket"`
	Fetch           FetchConfig   `json:"fetch"`
	Request         RequestConfig `json:"request"`
	FS              FSConfig      `json:"fs"`
}

func init() {
	var err error
	programCache, err = lru.New[string, *goja.Program](1024)
	if err != nil {
		panic(err)
	}
}

func FilterInstGoJa(gl core.Params) (core.FilterInstance, error) {
	var global Config
	global.EnableDebug = true
	global.EnableWebsocket = true
	global.Fetch.Enabled = true
	global.FS.Enabled = true
	if err := gl.Unmarshal(&global); err != nil {
		return nil, err
	}
	sharedClient := &http.Client{
		Timeout: 30 * time.Second,
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

			program, err := loadProgram(param.Exec, js)
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

			return debug.Flush(runProgram(ctx, jsLoop, program, request, global, sharedClient, debug, closers))
		}, nil
	}, nil
}

func loadProgram(execPath, js string) (*goja.Program, error) {
	hash := md5.Sum([]byte(js))
	cacheKey := fmt.Sprintf("%s:%x", execPath, hash)
	if program, ok := programCache.Get(cacheKey); ok {
		return program, nil
	}
	program, err := goja.Compile(execPath, js, false)
	if err != nil {
		return nil, err
	}
	programCache.Add(cacheKey, program)
	return program, nil
}

func runProgram(
	ctx core.FilterContext,
	jsLoop *eventloop.EventLoop,
	program *goja.Program,
	request *http.Request,
	global Config,
	sharedClient *http.Client,
	debug *DebugData,
	closers *Closers,
) error {
	resultCh := make(chan error, 1)
	var once sync.Once
	finish := func(err error) {
		once.Do(func() {
			resultCh <- err
		})
	}

	jsLoop.RunOnLoop(func(vm *goja.Runtime) {
		go func() {
			<-ctx.Done()
			vm.Interrupt("context done")
			finish(ctx.Err())
		}()

		if err := initRuntime(ctx, vm, request, global, sharedClient, debug, closers, jsLoop); err != nil {
			finish(errors.Join(err, errors.New("js init failed")))
			return
		}

		result, err := vm.RunProgram(program)
		if err != nil {
			finish(err)
			return
		}
		if promise, ok := exportPromise(result); ok {
			finishPromise(vm, promise, finish)
			return
		}
		finish(nil)
	})

	return <-resultCh
}

func initRuntime(
	ctx core.FilterContext,
	vm *goja.Runtime,
	request *http.Request,
	global Config,
	sharedClient *http.Client,
	debug *DebugData,
	closers *Closers,
	jsLoop *eventloop.EventLoop,
) error {
	url.Enable(vm)
	buffer.Enable(vm)
	if err := MetaInject(ctx, vm); err != nil {
		return err
	}
	if err := RequestInject(ctx, vm, request, global.Request); err != nil {
		return err
	}
	if global.FS.Enabled {
		if err := FSInject(ctx, vm); err != nil {
			return err
		}
	}
	if err := ResponseInject(vm, debug, request); err != nil {
		return err
	}
	if err := KVInject(ctx, vm); err != nil {
		return err
	}
	if err := EventInject(ctx, vm, jsLoop); err != nil {
		return err
	}
	if err := FetchInject(ctx, vm, jsLoop, sharedClient, global.Fetch); err != nil {
		return err
	}
	if global.EnableWebsocket {
		closer, err := WebsocketInject(ctx, vm, debug, request, jsLoop)
		if err != nil {
			return err
		}
		closers.AddCloser(closer.Close)
	}
	return nil
}

func exportPromise(result goja.Value) (*goja.Promise, bool) {
	if result == nil {
		return nil, false
	}
	promise, ok := result.Export().(*goja.Promise)
	return promise, ok
}

func finishPromise(vm *goja.Runtime, promise *goja.Promise, finish func(error)) {
	if err := errors.Join(
		vm.Set("__internal_resolve", func(goja.Value) { finish(nil) }),
		vm.Set("__internal_reject", func(reason goja.Value) { finish(errors.New(reason.String())) }),
		vm.Set("__internal_promise", promise),
	); err != nil {
		finish(err)
		return
	}
	if _, err := vm.RunString(`__internal_promise.then(__internal_resolve).catch(__internal_reject)`); err != nil {
		finish(err)
	}
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

func cloneBody(data []byte) io.ReadCloser {
	return io.NopCloser(bytes.NewReader(data))
}
