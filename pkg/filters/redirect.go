package filters

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

var portExp = regexp.MustCompile(`:\d+$`)

func FilterInstRedirect(g core.Params) (core.FilterInstance, error) {
	var global struct {
		Scheme string `json:"scheme"`
	}
	if err := g.Unmarshal(&global); err != nil {
		return nil, err
	}
	if global.Scheme == "" {
		global.Scheme = "https"
	}
	return func(config core.Params) (core.FilterCall, error) {
		var param struct {
			Targets []string `json:"targets"`
			Code    int      `json:"code"`
		}
		if err := config.Unmarshal(&param); err != nil {
			return nil, err
		}
		if len(param.Targets) == 0 {
			return nil, errors.New("no targets")
		}
		if param.Code == 0 {
			param.Code = http.StatusFound
		}
		if param.Code < 300 || param.Code > 399 {
			return nil, fmt.Errorf("invalid code: %d", param.Code)
		}
		return func(ctx core.FilterContext, writer http.ResponseWriter, request *http.Request, next core.NextCall) error {
			domain := portExp.ReplaceAllString(strings.ToLower(request.Host), "")
			if len(param.Targets) > 0 && !slices.Contains(ctx.Alias, domain) {
				// 重定向到配置的地址
				zap.L().Debug("redirect", zap.Any("src", request.Host), zap.Any("dst", param.Targets[0]))
				path := ctx.Path
				if strings.HasSuffix(path, "/index.html") || path == "index.html" {
					path = strings.TrimSuffix(path, "index.html")
				}
				target, err := url.Parse(fmt.Sprintf("%s://%s/%s", global.Scheme, param.Targets[0], path))
				if err != nil {
					return err
				}
				target.RawQuery = request.URL.RawQuery
				http.Redirect(writer, request, target.String(), param.Code)
				return nil
			}
			return next(ctx, writer, request)
		}, nil
	}, nil
}
