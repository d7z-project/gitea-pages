package quickjs

import (
	"fmt"
	"log"
	"strings"

	"github.com/buke/quickjs-go"
)

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
