package filters

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/pkg/errors"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

var portExp = regexp.MustCompile(`:\d+$`)

func FilterInstRedirect(_ core.GlobalFilterInit) (core.FilterInstance, error) {
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
				slog.Debug("redirect", "src", request.Host, "dst", param.Targets[0])
				path := ctx.Path
				if strings.HasSuffix(path, "/index.html") || path == "index.html" {
					path = strings.TrimSuffix(path, "index.html")
				}
				scheme := core.RequestInfoFromRequest(request).Scheme
				if scheme == "" {
					scheme = "http"
				}
				target := &url.URL{
					Scheme:   scheme,
					Host:     param.Targets[0],
					Path:     "/" + path,
					RawQuery: request.URL.RawQuery,
				}
				http.Redirect(writer, request, target.String(), param.Code)
				return nil
			}
			return next(ctx, writer, request)
		}, nil
	}, nil
}
