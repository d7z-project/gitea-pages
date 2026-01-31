package goja

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
)

func FetchInject(ctx context.Context, jsCtx *goja.Runtime, loop *eventloop.EventLoop, client *http.Client) error {
	return jsCtx.GlobalObject().Set("fetch", func(url string, options ...map[string]interface{}) *goja.Promise {
		promise, resolve, reject := jsCtx.NewPromise()

		go func() {
			method := "GET"
			var body io.Reader
			headers := make(http.Header)

			if len(options) > 0 {
				opts := options[0]
				if m, ok := opts["method"].(string); ok {
					method = strings.ToUpper(m)
				}
				if h, ok := opts["headers"].(map[string]interface{}); ok {
					for k, v := range h {
						if strVal, ok := v.(string); ok {
							headers.Set(k, strVal)
						}
					}
				}
				if b, ok := opts["body"].(string); ok {
					body = strings.NewReader(b)
				}
			}

			req, err := http.NewRequestWithContext(ctx, method, url, body)
			if err != nil {
				loop.RunOnLoop(func(*goja.Runtime) {
					_ = reject(err)
				})
				return
			}
			req.Header = headers

			resp, err := client.Do(req)
			if err != nil {
				loop.RunOnLoop(func(*goja.Runtime) {
					_ = reject(err)
				})
				return
			}
			defer resp.Body.Close()

			respBody, err := io.ReadAll(resp.Body)
			if err != nil {
				loop.RunOnLoop(func(*goja.Runtime) {
					_ = reject(err)
				})
				return
			}

			headersMap := make(map[string]interface{})
			for k, v := range resp.Header {
				headersMap[k] = v
			}

			loop.RunOnLoop(func(vm *goja.Runtime) {
				responseObj := map[string]interface{}{
					"ok":         resp.StatusCode >= 200 && resp.StatusCode < 300,
					"status":     resp.StatusCode,
					"statusText": resp.Status,
					"headers":    headersMap,
					"text": func() *goja.Promise {
						p, res, _ := vm.NewPromise()
						_ = res(string(respBody))
						return p
					},
					"json": func() *goja.Promise {
						p, res, rej := vm.NewPromise()
						var data interface{}
						if err := json.Unmarshal(respBody, &data); err != nil {
							_ = rej(err)
						} else {
							_ = res(data)
						}
						return p
					},
				}
				_ = resolve(responseObj)
			})
		}()

		return promise
	})
}
