package renders

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"text/template"

	sprig "github.com/go-task/slim-sprig/v3"
)

type GoTemplate struct{}

func init() {
	RegisterRender("gotemplate", &GoTemplate{})
}

func (g GoTemplate) Render(w http.ResponseWriter, r *http.Request, input io.Reader) error {
	dataB, err := io.ReadAll(input)
	if err != nil {
		return err
	}
	out := &bytes.Buffer{}
	parse, err := template.New("tmpl").Funcs(sprig.FuncMap()).Option("missingkey=error").Parse(string(dataB))
	headers := make(map[string]string)
	for k, vs := range r.Header {
		headers[k] = strings.Join(vs, ",")
	}
	if err != nil {
		return err
	}
	err = parse.Execute(out, map[string]interface{}{
		"Request": map[string]any{
			"Method":  r.Method,
			"Headers": headers,
		},
	})
	if err != nil {
		return err
	}
	_, err = out.WriteTo(w)
	return err
}
