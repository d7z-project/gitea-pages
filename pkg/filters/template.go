package filters

import (
	"bytes"
	"net/http"
	"path"
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
		prefix := path.Clean("/" + param.Prefix)
		if prefix == "/" {
			prefix = ""
		} else {
			prefix = strings.Trim(prefix, "/") + "/"
		}

		return func(ctx core.FilterContext, writer http.ResponseWriter, request *http.Request, next core.NextCall) error {
			data, err := ctx.ReadString(ctx, prefix+ctx.Path)
			if err != nil {
				return err
			}
			out := &bytes.Buffer{}
			parse, err := utils.NewTemplate().Funcs(map[string]any{
				"load": func(p string) (any, error) {
					fullPath := path.Clean("/" + p)
					return ctx.ReadString(ctx, prefix+strings.TrimPrefix(fullPath, "/"))
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
