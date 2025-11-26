package filters

import (
	"bytes"
	"net/http"
	"strings"

	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/gitea-pages/pkg/utils"
)

func FilterInstTemplate(_ core.Params) (core.FilterInstance, error) {
	return func(config core.Params) (core.FilterCall, error) {
		var param struct {
			Prefix string `json:"prefix"`
		}
		if err := config.Unmarshal(&param); err != nil {
			return nil, err
		}
		param.Prefix = strings.Trim(param.Prefix, "/") + "/"
		return func(ctx core.FilterContext, writer http.ResponseWriter, request *http.Request, next core.NextCall) error {
			data, err := ctx.ReadString(ctx, param.Prefix+ctx.Path)
			if err != nil {
				return err
			}
			out := &bytes.Buffer{}
			parse, err := utils.NewTemplate().Funcs(map[string]any{
				"load": func(path string) (any, error) {
					return ctx.ReadString(ctx, param.Prefix+path)
				},
			}).Parse(data)
			if err != nil {
				return err
			}
			err = parse.Execute(out, utils.NewTemplateInject(request, map[string]any{
				"Meta": map[string]string{
					"Org":    ctx.Owner,
					"Repo":   ctx.Repo,
					"Commit": ctx.CommitID,
				},
			}))
			if err != nil {
				return err
			}
			_, _ = out.WriteTo(writer)
			return nil
		}, nil
	}, nil
}
