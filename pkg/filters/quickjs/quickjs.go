package quickjs

import (
	"context"
	"fmt"
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
		Exec  string `json:"exec"`
		Debug bool   `json:"debug"`
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

		rt := quickjs.NewRuntime()
		rt.SetExecuteTimeout(5)
		defer rt.Close()

		jsCtx := rt.NewContext()
		defer jsCtx.Close()

		// 在 debug 模式下，我们需要拦截输出
		var (
			outputBuffer strings.Builder
			logBuffer    strings.Builder
			jsError      error
		)

		global := jsCtx.Globals()
		global.Set("request", createRequestObject(jsCtx, request, metadata))

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

		ret := jsCtx.Eval(js)
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

// renderDebugPage 渲染调试页面
func renderDebugPage(writer http.ResponseWriter, outputBuffer, logBuffer *strings.Builder, jsError error) error {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")

	html := `<!DOCTYPE html>
<html>
<head>
    <title>QuickJS Debug</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 0; padding: 20px; }
        .section { margin-bottom: 30px; border: 1px solid #ddd; border-radius: 5px; }
        .section-header { background: #f5f5f5; padding: 10px 15px; border-bottom: 1px solid #ddd; font-weight: bold; }
        .section-content { padding: 15px; background: white; }
        .output { white-space: pre-wrap; font-family: monospace; }
        .log { white-space: pre-wrap; font-family: monospace; background: #f8f8f8; }
        .error { color: #d00; background: #fee; padding: 10px; border-radius: 3px; }
        .success { color: #080; background: #efe; padding: 10px; border-radius: 3px; }
    </style>
</head>
<body>
    <h1>QuickJS Debug Output</h1>
    
    <div class="section">
        <div class="section-header">执行结果</div>
        <div class="section-content">
            <div class="output">`

	// 转义输出内容
	output := outputBuffer.String()
	if output == "" {
		output = "(无输出)"
	}
	html += htmlEscape(output)

	html += `</div>
        </div>
    </div>
    
    <div class="section">
        <div class="section-header">控制台日志</div>
        <div class="section-content">
            <div class="log">`

	// 转义日志内容
	logs := logBuffer.String()
	if logs == "" {
		logs = "(无日志)"
	}
	html += htmlEscape(logs)

	html += `</div>
        </div>
    </div>
    
    <div class="section">
        <div class="section-header">执行状态</div>
        <div class="section-content">`

	if jsError != nil {
		html += `<div class="error"><strong>错误:</strong> ` + htmlEscape(jsError.Error()) + `</div>`
	} else {
		html += `<div class="success">执行成功</div>`
	}

	html += `</div>
    </div>
</body>
</html>`

	_, err := writer.Write([]byte(html))
	return err
}

// htmlEscape 转义 HTML 特殊字符
func htmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	).Replace(s)
}

// createRequestObject 创建表示 HTTP 请求的 JavaScript 对象
func createRequestObject(ctx *quickjs.Context, req *http.Request, metadata *core.PageContent) *quickjs.Value {
	obj := ctx.NewObject()
	// 基本属性
	obj.Set("method", ctx.NewString(req.Method))
	url := *req.URL
	url.Path = metadata.Path
	obj.Set("url", ctx.NewString(url.String()))
	obj.Set("path", ctx.NewString(url.Path))
	obj.Set("rawPath", ctx.NewString(req.URL.Path))
	obj.Set("query", ctx.NewString(url.RawQuery))
	obj.Set("host", ctx.NewString(req.Host))
	obj.Set("remoteAddr", ctx.NewString(req.RemoteAddr))
	obj.Set("proto", ctx.NewString(req.Proto))
	obj.Set("httpVersion", ctx.NewString(req.Proto))

	// 解析查询参数
	queryObj := ctx.NewObject()
	for key, values := range url.Query() {
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
				names, err := headersObj.PropertyNames()
				if err != nil {
					return ctx.NewError(err)
				}
				for _, key := range names {
					writer.Header().Set(key, headersObj.Get(key).String())
				}
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
					cookie.Secure = secure.ToBool()
				}

				if httpOnly := options.Get("httpOnly"); !httpOnly.IsNull() {
					cookie.HttpOnly = httpOnly.ToBool()
				}
			}

			http.SetCookie(writer, cookie)
		}
		return ctx.NewNull()
	}))

	return obj
}

// createConsoleObject 创建 console 对象用于日志输出
func createConsoleObject(ctx *quickjs.Context, buf *strings.Builder) *quickjs.Value {
	console := ctx.NewObject()

	logFunc := func(level string, buffer *strings.Builder) func(*quickjs.Context, *quickjs.Value, []*quickjs.Value) *quickjs.Value {
		return func(q *quickjs.Context, value *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
			var messages []string
			for _, arg := range args {
				messages = append(messages, arg.String())
			}
			message := fmt.Sprintf("[%s] %s", level, strings.Join(messages, " "))

			// 总是输出到系统日志
			log.Print(message)

			// 如果有缓冲区，也写入缓冲区
			if buffer != nil {
				buffer.WriteString(message + "\n")
			}
			return ctx.NewNull()
		}
	}

	console.Set("log", ctx.NewFunction(logFunc("INFO", buf)))
	console.Set("info", ctx.NewFunction(logFunc("INFO", buf)))
	console.Set("warn", ctx.NewFunction(logFunc("WARN", buf)))
	console.Set("error", ctx.NewFunction(logFunc("ERROR", buf)))
	console.Set("debug", ctx.NewFunction(logFunc("DEBUG", buf)))
	return console
}
