package filters

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/gitea-pages/pkg/utils"
)

var FilterInstTemplate core.FilterInstance = func(config core.FilterParams) (core.FilterCall, error) {
	var param struct {
		Prefix string `json:"prefix"`
	}
	if err := config.Unmarshal(&param); err != nil {
		return nil, err
	}
	param.Prefix = strings.Trim(param.Prefix, "/") + "/"
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, metadata *core.PageContent, next core.NextCall) error {
		data, err := metadata.ReadString(ctx, param.Prefix+metadata.Path)
		if err != nil {
			return err
		}
		if err != nil {
			return err
		}
		out := &bytes.Buffer{}
		parse, err := utils.NewTemplate().Funcs(map[string]any{
			"load": func(path string) (any, error) {
				return metadata.ReadString(ctx, param.Prefix+path)
			},
		}).Parse(data)
		if err != nil {
			return err
		}
		err = parse.Execute(out, utils.NewTemplateInject(request, nil))
		if err != nil {
			return err
		}
		_, _ = out.WriteTo(writer)
		return nil
	}, nil
}
