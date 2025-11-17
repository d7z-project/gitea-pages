package goja

import (
	"bytes"
	_ "embed"
	"html/template"
	"net/http"
	"time"
)

//go:embed debug.tmpl
var errorPageStr string

var errorPage = template.Must(template.New("error.tmpl").Parse(errorPageStr))

type DebugData struct {
	enabled bool
	status  int
	headers http.Header
	body    *bytes.Buffer
	logs    []debugDataLog

	request *http.Request
	parent  http.ResponseWriter
}

func NewDebug(debug bool, request *http.Request, writer http.ResponseWriter) *DebugData {
	return &DebugData{
		enabled: debug,
		status:  http.StatusOK,
		headers: make(http.Header),
		body:    new(bytes.Buffer),
		logs:    []debugDataLog{},
		parent:  writer,
		request: request,
	}
}

type debugDataLog struct {
	Level   string
	Time    time.Time
	Message string
}

func (d *DebugData) Log(msg string) {
	if !d.enabled {
		return
	}
	d.logs = append(d.logs, debugDataLog{
		Level:   "log",
		Time:    time.Now(),
		Message: msg,
	})
}

func (d *DebugData) Warn(msg string) {
	if !d.enabled {
		return
	}
	d.logs = append(d.logs, debugDataLog{
		Level:   "warn",
		Time:    time.Now(),
		Message: msg,
	})
}

func (d *DebugData) Error(msg string) {
	if !d.enabled {
		return
	}
	d.logs = append(d.logs, debugDataLog{
		Level:   "error",
		Time:    time.Now(),
		Message: msg,
	})
}

func (d *DebugData) Header() http.Header {
	if d.enabled {
		return d.headers
	}
	return d.parent.Header()
}

func (d *DebugData) Write(i []byte) (int, error) {
	if d.enabled {
		return d.body.Write(i)
	}
	return d.parent.Write(i)
}

func (d *DebugData) WriteHeader(statusCode int) {
	if d.enabled {
		d.status = statusCode
	} else {
		d.parent.WriteHeader(statusCode)
	}
}

func (d *DebugData) Flush(err error) error {
	if !d.enabled {
		return err
	}
	data := map[string]interface{}{
		"Logs":    d.logs,
		"Output":  d.body.String(),
		"Headers": d.Header(),
	}
	d.body.Reset()
	if err != nil {
		d.status = http.StatusInternalServerError
		data["Error"] = err.Error()
	}
	d.parent.Header().Set("Content-Type", "text/html; charset=utf-8")
	d.parent.WriteHeader(d.status)
	_ = errorPage.Execute(d.parent, data)
	return nil
}
