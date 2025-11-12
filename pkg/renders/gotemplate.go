package renders

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"gopkg.d7z.net/gitea-pages/pkg/core"

	"gopkg.d7z.net/gitea-pages/pkg/utils"
)

type GoTemplate struct{}

func init() {
}

func (g GoTemplate) Render(ctx context.Context, w http.ResponseWriter, r *http.Request, input io.Reader, meta *core.PageDomainContent) error {
	data, err := io.ReadAll(input)
	if err != nil {
		return err
	}
	out := &bytes.Buffer{}
	parse, err := utils.NewTemplate().Funcs(map[string]any{
		"template": func(path string) (any, error) {
			return meta.ReadString(ctx, path)
		},
	}).Parse(string(data))
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
