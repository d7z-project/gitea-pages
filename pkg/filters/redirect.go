package filters

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"go.uber.org/zap"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

var portExp = regexp.MustCompile(`:\d+$`)

var FilterInstRedirect core.FilterInstance = func(config core.FilterParams) (core.FilterCall, error) {
	var param struct {
		Targets []string `json:"targets"`
		Code    int      `json:"code"`
	}
	if err := config.Unmarshal(&param); err != nil {
		return nil, err
	}
	if len(param.Targets) == 0 {
		return nil, fmt.Errorf("no targets")
	}
	if param.Code == 0 {
		param.Code = http.StatusFound
	}
	if param.Code < 300 || param.Code > 399 {
		return nil, fmt.Errorf("invalid code: %d", param.Code)
	}
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, metadata *core.PageContent, next core.NextCall) error {
		domain := portExp.ReplaceAllString(strings.ToLower(request.Host), "")
		if len(param.Targets) > 0 && !slices.Contains(metadata.Alias, domain) {
			// 重定向到配置的地址
			zap.L().Debug("redirect", zap.Any("src", request.Host), zap.Any("dst", param.Targets[0]))
			path := metadata.Path
			if strings.HasSuffix(path, "/index.html") || path == "index.html" {
				path = strings.TrimSuffix(path, "index.html")
			}
			target, err := url.Parse(fmt.Sprintf("https://%s/%s", param.Targets[0], path))
			if err != nil {
				return err
			}
			target.RawQuery = request.URL.RawQuery

			http.Redirect(writer, request, target.String(), param.Code)
			return nil
		} else {
			return next(ctx, writer, request, metadata)
		}
	}, nil
}
