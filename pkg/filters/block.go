package filters

import (
	"net/http"

	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func FilterInstBlock(_ core.Params) (core.FilterInstance, error) {
	return func(config core.Params) (core.FilterCall, error) {
		var param struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		if err := config.Unmarshal(&param); nil != err {
			return nil, err
		}
		if param.Code == 0 {
			param.Code = http.StatusForbidden
		}
		if param.Message == "" {
			param.Message = http.StatusText(param.Code)
		}
		return func(ctx core.FilterContext, writer http.ResponseWriter, request *http.Request, next core.NextCall) error {
			writer.WriteHeader(param.Code)
			if param.Message != "" {
				_, _ = writer.Write([]byte(param.Message))
			}
			return nil
		}, nil
	}, nil
}
