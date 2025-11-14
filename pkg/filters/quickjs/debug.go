package quickjs

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/buke/quickjs-go"
)

type DebugData struct {
	Status int            `json:"status"`
	Header http.Header    `json:"header"`
	Body   string         `json:"body"`
	Logs   []DebugDataLog `json:"logs"`
}

type DebugDataLog struct {
	Level   string    `json:"level"`
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
}

// debugResponseWriter 用于在 debug 模式下捕获响应输出
type debugResponseWriter struct {
	buffer *strings.Builder
	header http.Header
	status int
}

func (w *debugResponseWriter) Header() http.Header {
	return w.header
}

func (w *debugResponseWriter) Write(data []byte) (int, error) {
	return w.buffer.Write(data)
}

func (w *debugResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
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
		html += `<div class="error"><pre><code><strong>Message:</strong></br>`
		var q *quickjs.Error
		if errors.As(jsError, &q) {
			html += q.Message + "</br></br>"
			html += `<strong>Stack:</strong></br>` + q.Stack
		} else {
			html += jsError.Error()
		}

		html += `</pre></code></div>`
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
