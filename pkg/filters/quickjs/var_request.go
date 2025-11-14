package quickjs

import (
	"io"
	"net/http"

	"github.com/buke/quickjs-go"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

// createRequestObject 创建表示 HTTP 请求的 JavaScript 对象
func createRequestObject(ctx *quickjs.Context, req *http.Request, filterCtx core.FilterContext) *quickjs.Value {
	obj := ctx.NewObject()
	// 基本属性
	obj.Set("method", ctx.NewString(req.Method))
	url := *req.URL
	url.Path = filterCtx.Path
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
