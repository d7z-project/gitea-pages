package quickjs

import (
	"net/http"
	"time"

	"github.com/buke/quickjs-go"
)

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
