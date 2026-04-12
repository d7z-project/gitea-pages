package goja

import (
	"bytes"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
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

type RealtimeConfig struct {
	WebSocket bool `json:"websocket"`
	SSE       bool `json:"sse"`
}

type Config struct {
	EnableDebug bool           `json:"debug"`
	Realtime    RealtimeConfig `json:"realtime"`
	Fetch       FetchConfig    `json:"fetch"`
	Request     RequestConfig  `json:"request"`
	FS          FSConfig       `json:"fs"`
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
	global.Realtime.WebSocket = true
	global.Realtime.SSE = true
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

			jsLoop := eventloop.NewEventLoop(eventloop.EnableConsole(true))

			jsLoop.Start()
			defer jsLoop.Stop()

			closers := NewClosers()
			defer closers.Close()

			return debug.Flush(runProgram(ctx, jsLoop, program, request, w, global, sharedClient, debug, closers))
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
	writer http.ResponseWriter,
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

		requestObj, err := initRuntime(ctx, vm, request, global, sharedClient, debug, closers, jsLoop)
		if err != nil {
			finish(errors.Join(err, errors.New("js init failed")))
			return
		}

		if _, err = vm.RunProgram(program); err != nil {
			finish(err)
			return
		}
		result, err := callHandler(vm, requestObj)
		if err != nil {
			finish(err)
			return
		}
		if upgrade, ok := upgradeResponseValue(vm, result); ok {
			go func() {
				finish(upgrade())
			}()
			return
		}
		if promise, ok := exportPromise(result); ok {
			finishPromise(vm, promise, func(value goja.Value) {
				if upgrade, ok := upgradeResponseValue(vm, value); ok {
					go func() {
						finish(upgrade())
					}()
					return
				}
				finish(writeResponseValue(vm, writer, value))
			}, finish)
			return
		}
		finish(writeResponseValue(vm, writer, result))
	})

	return <-resultCh
}

func exportPromise(result goja.Value) (*goja.Promise, bool) {
	if result == nil {
		return nil, false
	}
	promise, ok := result.Export().(*goja.Promise)
	return promise, ok
}

func finishPromise(vm *goja.Runtime, promise *goja.Promise, resolveValue func(goja.Value), finish func(error)) {
	if err := errors.Join(
		vm.Set("__internal_resolve", func(value goja.Value) { resolveValue(value) }),
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
