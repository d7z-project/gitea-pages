package utils

import (
	"net/http"
	"strings"
	"text/template"

	sprig "github.com/go-task/slim-sprig/v3"
)

func NewTemplateInject(r *http.Request, def map[string]any) map[string]any {
	if def == nil {
		def = make(map[string]any)
	}
	headers := make(map[string]string)
	for k, vs := range r.Header.Clone() {
		headers[k] = strings.Join(vs, ",")
	}
	def["Request"] = map[string]any{
		"Host":     r.Host,
		"Path":     r.URL.Path,
		"Params":   r.URL.Query(),
		"Method":   r.Method,
		"RemoteIP": GetRemoteIP(r),
	}
	return def
}

func MustTemplate(data string) *template.Template {
	newTemplate, err := NewTemplate(data)
	if err != nil {
		panic(err)
	}
	return newTemplate
}

func NewTemplate(data string) (*template.Template, error) {
	funcMap := sprig.FuncMap()
	delete(funcMap, "env")
	delete(funcMap, "expandenv")
	t := template.New("tmpl").Funcs(funcMap)
	return t.Parse(data)
}
