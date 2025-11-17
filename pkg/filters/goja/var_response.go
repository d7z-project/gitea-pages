package goja

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/dop251/goja"
)

func ResponseInject(jsCtx *goja.Runtime, writer http.ResponseWriter, req *http.Request) error {
	return jsCtx.GlobalObject().Set("response", map[string]any{
		// 响应头操作
		"setHeader": func(key string, value string) {
			writer.Header().Set(key, value)
		},

		"getHeader": func(key string) string {
			return writer.Header().Get(key)
		},

		"removeHeader": func(key string) {
			writer.Header().Del(key)
		},

		"hasHeader": func(key string) bool {
			_, exists := writer.Header()[key]
			return exists
		},

		// 状态码操作
		"setStatus": func(statusCode int) {
			writer.WriteHeader(statusCode)
		},

		"statusCode": func(statusCode int) {
			writer.WriteHeader(statusCode)
		},

		// 写入响应
		"write": func(data string) {
			_, err := writer.Write([]byte(data))
			if err != nil {
				panic(err)
			}
		},

		"writeHead": func(statusCode int, headers ...map[string]string) {
			// 处理可选的 headers 参数
			if len(headers) > 0 {
				for key, value := range headers[0] {
					writer.Header().Set(key, value)
				}
			}
			writer.WriteHeader(statusCode)
		},

		"end": func(data ...string) {
			if len(data) > 0 {
				_, err := writer.Write([]byte(data[0]))
				if err != nil {
					panic(err)
				}
			}
			// 在实际的 HTTP 处理中，我们通常不手动结束响应
		},

		// 重定向
		"redirect": func(location string, statusCode ...int) {
			code := 302
			if len(statusCode) > 0 {
				code = statusCode[0]
			}
			http.Redirect(writer, req, location, code)
		},

		// JSON 响应
		"json": func(data goja.Value) {
			writer.Header().Set("Content-Type", "application/json")

			var jsonStr string
			export := data.Export()
			switch v := export.(type) {
			case string:
				jsonStr = v
			default:
				marshal, err := json.Marshal(v)
				if err != nil {
					panic(err)
				}
				jsonStr = string(marshal)
			}
			_, err := writer.Write([]byte(jsonStr))
			if err != nil {
				panic(err)
			}
		},

		// 设置 cookie
		"setCookie": func(name string, value string, options ...map[string]interface{}) {
			cookie := &http.Cookie{
				Name:  name,
				Value: value,
				Path:  "/",
			}

			// 处理可选参数
			if len(options) > 0 && options[0] != nil {
				opts := options[0]

				if maxAge, ok := opts["maxAge"].(int); ok {
					cookie.MaxAge = maxAge
				}

				if expires, ok := opts["expires"].(int64); ok {
					cookie.Expires = time.Unix(expires, 0)
				}

				if path, ok := opts["path"].(string); ok {
					cookie.Path = path
				}

				if domain, ok := opts["domain"].(string); ok {
					cookie.Domain = domain
				}

				if secure, ok := opts["secure"].(bool); ok {
					cookie.Secure = secure
				}

				if httpOnly, ok := opts["httpOnly"].(bool); ok {
					cookie.HttpOnly = httpOnly
				}

				if sameSite, ok := opts["sameSite"].(string); ok {
					switch sameSite {
					case "lax":
						cookie.SameSite = http.SameSiteLaxMode
					case "strict":
						cookie.SameSite = http.SameSiteStrictMode
					case "none":
						cookie.SameSite = http.SameSiteNoneMode
					}
				}
			}
			http.SetCookie(writer, cookie)
		},
	})
}
