package quickjs

import (
	"bytes"
	_ "embed"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/buke/quickjs-go"
	"github.com/pkg/errors"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

//go:embed inject.js
var inject string

var FilterInstQuickJS core.FilterInstance = func(config core.FilterParams) (core.FilterCall, error) {
	var param struct {
		Exec  string `json:"exec"`
		Debug bool   `json:"debug"`
	}
	if err := config.Unmarshal(&param); err != nil {
		return nil, err
	}
	if param.Exec == "" {
		return nil, errors.Errorf("no exec specified")
	}
	return func(ctx core.FilterContext, writer http.ResponseWriter, request *http.Request, next core.NextCall) error {
		js, err := ctx.ReadString(ctx, param.Exec)
		if err != nil {
			return err
		}

		rt := quickjs.NewRuntime()
		rt.SetExecuteTimeout(5)
		rt.SetMemoryLimit(10 * 1024 * 1024)
		defer rt.Close()
		jsCtx := rt.NewContext()
		defer jsCtx.Close()
		cacheKey := "qjs/" + param.Exec
		var bytecode []byte
		cacheData, err := ctx.Cache.Get(ctx, cacheKey)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if bytecode, err = jsCtx.Compile(js,
				quickjs.EvalFlagCompileOnly(true),
				quickjs.EvalFileName(param.Exec)); err != nil {
				return err
			}
			if err = ctx.Cache.Put(ctx, cacheKey, map[string]string{}, bytes.NewBuffer(bytecode)); err != nil {
				return err
			}
		} else {
			defer cacheData.Close()
			if bytecode, err = io.ReadAll(cacheData); err != nil {
				return err
			}
		}
		// 在 debug 模式下，我们需要拦截输出
		var (
			outputBuffer strings.Builder
			logBuffer    strings.Builder
			jsError      error
		)

		global := jsCtx.Globals()
		global.Set("request", createRequestObject(jsCtx, request, ctx))
		// 根据是否 debug 模式创建不同的 response 对象
		if param.Debug {
			// debug 模式下使用虚假的 writer 来捕获输出
			global.Set("response", createResponseObject(jsCtx, &debugResponseWriter{
				buffer: &outputBuffer,
				header: make(http.Header),
			}, request))
			global.Set("console", createConsoleObject(jsCtx, &logBuffer))
		} else {
			global.Set("response", createResponseObject(jsCtx, writer, request))
			global.Set("console", createConsoleObject(jsCtx, nil))
		}
		jsCtx.Eval(inject)
		ret := jsCtx.EvalBytecode(bytecode)
		defer ret.Free()
		jsCtx.Loop()

		if ret.IsException() {
			err := jsCtx.Exception()
			jsError = err
		}

		// 如果在 debug 模式下，返回 HTML 调试页面
		if param.Debug {
			return renderDebugPage(writer, &outputBuffer, &logBuffer, jsError)
		}

		return jsError
	}, nil
}
