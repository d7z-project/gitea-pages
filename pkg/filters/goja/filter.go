package goja

import (
	"crypto/md5"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	lru "github.com/hashicorp/golang-lru/v2"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

var programCache *lru.Cache[string, *goja.Program]

const (
	defaultRequestBodyLimit int64 = 4 << 20
	defaultFetchBodyLimit   int64 = 4 << 20
	runtimeShutdownTimeout        = 2 * time.Second
)

var (
	errMissingHandler = errors.New("missing handler registration; call serve(handler)")
	errInvalidHandler = errors.New("handler must be a function or an object with fetch(request)")
)

type FetchConfig struct {
	Enabled              bool     `json:"enabled"`
	MaxResponseBodyBytes int64    `json:"max_response_body_bytes"`
	AllowedHosts         []string `json:"allowed_hosts"`
	BlockPrivateNetwork  bool     `json:"block_private_network"`
}

type RequestConfig struct {
	MaxBodyBytes int64 `json:"max_body_bytes"`
}

type RealtimeConfig struct {
	EventBuffer int `json:"event_buffer"`
}

type Config struct {
	EnableDebug bool           `json:"debug"`
	Realtime    RealtimeConfig `json:"realtime"`
	Fetch       FetchConfig    `json:"fetch"`
	Request     RequestConfig  `json:"request"`
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
	global.Realtime.EventBuffer = defaultEventPendingLimit
	global.Fetch.Enabled = true
	global.Fetch.MaxResponseBodyBytes = defaultFetchBodyLimit
	global.Request.MaxBodyBytes = defaultRequestBodyLimit
	if err := gl.Unmarshal(&global); err != nil {
		return nil, err
	}
	sharedClient := newFetchClient(global.Fetch)
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
	runtime := &runtimeState{}
	defer func() {
		runtime.beginClosing()
		_ = closers.Close()
		_ = runtime.wait(runtimeShutdownTimeout)
	}()

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
			runtime.beginClosing()
			_ = closers.Close()
			vm.Interrupt("context done")
			timer := time.NewTimer(20 * time.Millisecond)
			defer timer.Stop()
			<-timer.C
			finish(ctx.Err())
		}()

		requestObj, err := initRuntime(ctx, vm, request, global, sharedClient, debug, closers, jsLoop, runtime)
		if err != nil {
			finish(errors.Join(err, errors.New("js init failed")))
			return
		}

		if _, err = vm.RunProgram(program); err != nil {
			finish(err)
			return
		}
		result, err := callRegisteredHandler(vm, requestObj)
		if err != nil {
			finish(err)
			return
		}
		complete := func(value goja.Value) {
			if upgrade, ok := upgradeResponseValue(vm, value); ok {
				go func() {
					finish(upgrade())
				}()
				return
			}
			write := func() {
				finish(writeResponseValue(vm, writer, value))
			}
			if responseWritesAsync(vm, value) {
				go write()
				return
			}
			write()
		}
		if promise, ok := exportPromise(result); ok {
			finishPromise(vm, promise, complete, finish)
			return
		}
		complete(result)
	})

	return <-resultCh
}

func callRegisteredHandler(vm *goja.Runtime, requestObj *goja.Object) (goja.Value, error) {
	handler := vm.Get(internalHandlerName)
	if isNilish(handler) {
		return nil, errMissingHandler
	}
	fetchFn, thisValue, err := resolveHandler(vm, handler)
	if err != nil {
		return nil, err
	}
	return fetchFn(thisValue, requestObj)
}

func responseWritesAsync(vm *goja.Runtime, value goja.Value) bool {
	state, ok := responseStateFromValue(vm, value)
	return ok && state.stream != nil
}

func exportPromise(result goja.Value) (*goja.Promise, bool) {
	if isNilish(result) {
		return nil, false
	}
	promise, ok := result.Export().(*goja.Promise)
	return promise, ok
}

func finishPromise(vm *goja.Runtime, promise *goja.Promise, resolveValue func(goja.Value), finish func(error)) {
	if err := errors.Join(
		vm.Set("__internal_resolve", func(value goja.Value) { resolveValue(value) }),
		vm.Set("__internal_reject", func(reason goja.Value) {
			if isNilish(reason) {
				finish(errors.New("promise rejected"))
				return
			}
			finish(errors.New(reason.String()))
		}),
		vm.Set("__internal_promise", promise),
	); err != nil {
		finish(err)
		return
	}
	if _, err := vm.RunString(`__internal_promise.then(__internal_resolve).catch(__internal_reject)`); err != nil {
		finish(err)
	}
}
