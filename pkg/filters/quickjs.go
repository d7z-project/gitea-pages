package filters

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/buke/quickjs-go"
	"github.com/pkg/errors"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

var FilterInstQuickJS core.FilterInstance = func(config core.FilterParams) (core.FilterCall, error) {
	var param struct {
		Exec string `json:"exec"`
	}
	if err := config.Unmarshal(&param); err != nil {
		return nil, err
	}
	if param.Exec == "" {
		return nil, errors.Errorf("no exec specified")
	}
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, metadata *core.PageContent, next core.NextCall) error {
		js, err := metadata.ReadString(ctx, param.Exec)
		if err != nil {
			return err
		}

		var rt = quickjs.NewRuntime()
		defer rt.Close()

		jsCtx := rt.NewContext()
		defer jsCtx.Close()

		global := jsCtx.Globals()
		global.Set("request", createRequestObject(jsCtx, request))
		global.Set("response", createResponseObject(jsCtx, writer, request))
		global.Set("console", createConsoleObject(jsCtx))

		ret := jsCtx.Eval(js)
		defer ret.Free()

		if ret.IsException() {
			err := jsCtx.Exception()
			return err
		}
		return nil
	}, nil
}

// createRequestObject 创建表示 HTTP 请求的 JavaScript 对象
func createRequestObject(ctx *quickjs.Context, req *http.Request) *quickjs.Value {
	obj := ctx.NewObject()

	// 基本属性
	obj.Set("method", ctx.NewString(req.Method))
	obj.Set("url", ctx.NewString(req.URL.String()))
	obj.Set("path", ctx.NewString(req.URL.Path))
	obj.Set("query", ctx.NewString(req.URL.RawQuery))
	obj.Set("host", ctx.NewString(req.Host))
	obj.Set("remoteAddr", ctx.NewString(req.RemoteAddr))
	obj.Set("proto", ctx.NewString(req.Proto))
	obj.Set("httpVersion", ctx.NewString(req.Proto))

	// 解析查询参数
	queryObj := ctx.NewObject()
	for key, values := range req.URL.Query() {
		if len(values) > 0 {
			queryObj.Set(key, ctx.NewString(values[0]))
		}
	}
	obj.Set("query", queryObj)

	// 添加 headers
	headersObj := ctx.NewObject()
	for key, values := range req.Header {
		if len(values) > 0 {
			headersObj.Set(key, ctx.NewString(values[0]))
		}
	}
	obj.Set("headers", headersObj)

	// 请求方法
	obj.Set("get", ctx.NewFunction(func(c *quickjs.Context, value *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) > 0 {
			key := args[0].String()
			return ctx.NewString(req.Header.Get(key))
		}
		return ctx.NewNull()
	}))

	// 读取请求体
	obj.Set("readBody", ctx.NewFunction(func(c *quickjs.Context, value *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return ctx.NewError(err)
		}
		return ctx.NewString(string(body))
	}))

	return obj
}

// createResponseObject 创建表示 HTTP 响应的 JavaScript 对象
func createResponseObject(ctx *quickjs.Context, writer http.ResponseWriter, req *http.Request) *quickjs.Value {
	obj := ctx.NewObject()

	// 响应头操作
	obj.Set("setHeader", ctx.NewFunction(func(c *quickjs.Context, value *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) >= 2 {
			key := args[0].String()
			value := args[1].String()
			writer.Header().Set(key, value)
		}
		return ctx.NewNull()
	}))

	obj.Set("getHeader", ctx.NewFunction(func(c *quickjs.Context, value *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) > 0 {
			key := args[0].String()
			return ctx.NewString(writer.Header().Get(key))
		}
		return ctx.NewNull()
	}))

	obj.Set("removeHeader", ctx.NewFunction(func(c *quickjs.Context, value *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) > 0 {
			key := args[0].String()
			writer.Header().Del(key)
		}
		return ctx.NewNull()
	}))

	obj.Set("hasHeader", ctx.NewFunction(func(c *quickjs.Context, value *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) > 0 {
			key := args[0].String()
			_, exists := writer.Header()[key]
			return ctx.NewBool(exists)
		}
		return ctx.NewBool(false)
	}))

	// 状态码操作
	obj.Set("setStatus", ctx.NewFunction(func(c *quickjs.Context, value *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) > 0 {
			writer.WriteHeader(int(args[0].ToInt32()))
		}
		return ctx.NewNull()
	}))

	obj.Set("statusCode", ctx.NewFunction(func(c *quickjs.Context, value *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) > 0 {
			writer.WriteHeader(int(args[0].ToInt32()))
		}
		return ctx.NewNull()
	}))

	// 写入响应
	obj.Set("write", ctx.NewFunction(func(c *quickjs.Context, value *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) > 0 {
			data := args[0].String()
			_, err := writer.Write([]byte(data))
			if err != nil {
				return ctx.NewError(err)
			}
		}
		return ctx.NewNull()
	}))

	obj.Set("writeHead", ctx.NewFunction(func(c *quickjs.Context, value *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) >= 1 {
			statusCode := int(args[0].ToInt32())

			// 处理可选的 headers 参数
			if len(args) >= 2 && args[1].IsObject() {
				headersObj := args[1]
				headersObj.Properties().ForEach(func(key string, value *quickjs.Value) bool {
					writer.Header().Set(key, value.String())
					return true
				})
			}

			writer.WriteHeader(statusCode)
		}
		return ctx.NewNull()
	}))

	obj.Set("end", ctx.NewFunction(func(c *quickjs.Context, value *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) > 0 {
			data := args[0].String()
			_, err := writer.Write([]byte(data))
			if err != nil {
				return ctx.NewError(err)
			}
		}
		// 在实际的 HTTP 处理中，我们通常不手动结束响应
		return ctx.NewNull()
	}))

	// 重定向
	obj.Set("redirect", ctx.NewFunction(func(c *quickjs.Context, value *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) > 0 {
			location := args[0].String()
			statusCode := 302
			if len(args) > 1 {
				statusCode = int(args[1].ToInt32())
			}
			http.Redirect(writer, req, location, statusCode)
		}
		return ctx.NewNull()
	}))

	// JSON 响应
	obj.Set("json", ctx.NewFunction(func(c *quickjs.Context, value *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) > 0 {
			writer.Header().Set("Content-Type", "application/json")
			jsonData := args[0].String()
			_, err := writer.Write([]byte(jsonData))
			if err != nil {
				return ctx.NewError(err)
			}
		}
		return ctx.NewNull()
	}))

	// 设置 cookie
	obj.Set("setCookie", ctx.NewFunction(func(c *quickjs.Context, value *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) >= 2 {
			name := args[0].String()
			value := args[1].String()

			cookie := &http.Cookie{
				Name:  name,
				Value: value,
				Path:  "/",
			}

			// 处理可选参数
			if len(args) >= 3 && args[2].IsObject() {
				options := args[2]

				if maxAge := options.Get("maxAge"); !maxAge.IsNull() {
					cookie.MaxAge = int(maxAge.ToInt32())
				}

				if expires := options.Get("expires"); !expires.IsNull() {
					if expires.IsNumber() {
						cookie.Expires = time.Unix(expires.ToInt64(), 0)
					}
				}

				if path := options.Get("path"); !path.IsNull() {
					cookie.Path = path.String()
				}

				if domain := options.Get("domain"); !domain.IsNull() {
					cookie.Domain = domain.String()
				}

				if secure := options.Get("secure"); !secure.IsNull() {
					cookie.Secure = secure.Bool()
				}

				if httpOnly := options.Get("httpOnly"); !httpOnly.IsNull() {
					cookie.HttpOnly = httpOnly.Bool()
				}
			}

			http.SetCookie(writer, cookie)
		}
		return ctx.NewNull()
	}))

	return obj
}

// createConsoleObject 创建 console 对象用于日志输出
func createConsoleObject(ctx *quickjs.Context) *quickjs.Value {
	console := ctx.NewObject()

	logFunc := func(level string) func(*quickjs.Context, *quickjs.Value, []*quickjs.Value) *quickjs.Value {
		return func(q *quickjs.Context, value *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
			var messages []string
			for _, arg := range args {
				messages = append(messages, arg.String())
			}
			log.Printf("[" + level + "] " + strings.Join(messages, " "))
			return ctx.NewNull()
		}
	}

	console.Set("log", ctx.NewFunction(logFunc("INFO")))
	console.Set("info", ctx.NewFunction(logFunc("INFO")))
	console.Set("warn", ctx.NewFunction(logFunc("WARN")))
	console.Set("error", ctx.NewFunction(logFunc("ERROR")))
	console.Set("debug", ctx.NewFunction(logFunc("DEBUG")))

	// 添加 time 和 timeEnd 方法用于性能测量
	timers := make(map[string]time.Time)

	console.Set("time", ctx.NewFunction(func(c *quickjs.Context, value *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) > 0 {
			label := args[0].String()
			timers[label] = time.Now()
		}
		return ctx.NewNull()
	}))

	console.Set("timeEnd", ctx.NewFunction(func(c *quickjs.Context, value *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		if len(args) > 0 {
			label := args[0].String()
			if start, exists := timers[label]; exists {
				elapsed := time.Since(start)
				log.Printf("[TIMER] %s: %v", label, elapsed)
				delete(timers, label)
			}
		}
		return ctx.NewNull()
	}))

	return console
}
