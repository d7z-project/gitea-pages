package renders

import (
	"bytes"
	"io"
	"net/http"

	"gopkg.d7z.net/gitea-pages/pkg/core"

	"gopkg.d7z.net/gitea-pages/pkg/utils"
)

type GoTemplate struct{}

func init() {
	core.RegisterRender("gotemplate", &GoTemplate{})
}

func (g GoTemplate) Render(w http.ResponseWriter, r *http.Request, input io.Reader) error {
	dataB, err := io.ReadAll(input)
	if err != nil {
		return err
	}
	out := &bytes.Buffer{}

	parse, err := utils.NewTemplate(string(dataB))
	if err != nil {
		return err
	}
	err = parse.Execute(out, utils.NewTemplateInject(r, nil))
	if err != nil {
		return err
	}
	_, err = out.WriteTo(w)
	return err
}
