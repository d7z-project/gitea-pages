package filters

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"go.uber.org/zap"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/gitea-pages/pkg/utils"
)

func FilterInstProxy(_ core.Params) (core.FilterInstance, error) {
	return func(config core.Params) (core.FilterCall, error) {
		var param struct {
			Prefix string `json:"prefix"`
			Target string `json:"target"`
		}
		if err := config.Unmarshal(&param); err != nil {
			return nil, err
		}
		return func(ctx core.FilterContext, writer http.ResponseWriter, request *http.Request, next core.NextCall) error {
			proxyPath := "/" + ctx.Path
			targetPath := strings.TrimPrefix(proxyPath, param.Prefix)
			if !strings.HasPrefix(targetPath, "/") {
				targetPath = "/" + targetPath
			}
			u, _ := url.Parse(param.Target)
			request.URL.Path = targetPath
			request.RequestURI = request.URL.RequestURI()
			proxy := httputil.NewSingleHostReverseProxy(u)
			// todo: 处理透传
			// proxy.Transport = s.options.HTTPClient.Transport
			if host, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
				request.Header.Set("X-Real-IP", host)
			}
			request.Header.Set("X-Page-IP", utils.GetRemoteIP(request))
			request.Header.Set("X-Page-Refer", fmt.Sprintf("%s/%s/%s", ctx.Owner, ctx.Repo, ctx.Path))
			request.Header.Set("X-Page-Host", request.Host)
			zap.L().Debug("命中反向代理", zap.Any("prefix", param.Prefix), zap.Any("target", param.Target),
				zap.Any("path", proxyPath), zap.Any("target", fmt.Sprintf("%s%s", u, targetPath)))
			// todo(security): 处理 websocket
			proxy.ServeHTTP(writer, request)
			return nil
		}, nil
	}, nil
}
