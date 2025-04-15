package renders

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"strings"
	"text/template"

	sprig "github.com/go-task/slim-sprig/v3"
)

type GoTemplate struct{}

func init() {
	RegisterRender("gotemplate", &GoTemplate{})
}

func (g GoTemplate) Render(w http.ResponseWriter, r *http.Request, input io.Reader) error {
	dataB, err := io.ReadAll(input)
	if err != nil {
		return err
	}
	out := &bytes.Buffer{}
	parse, err := template.New("tmpl").Funcs(sprig.FuncMap()).Option("missingkey=error").Parse(string(dataB))
	headers := make(map[string]string)
	for k, vs := range r.Header {
		headers[k] = strings.Join(vs, ",")
	}
	if err != nil {
		return err
	}
	err = parse.Execute(out, map[string]interface{}{
		"Request": map[string]any{
			"Headers":    headers,
			"Request":    r.RequestURI,
			"RemoteAddr": r.RemoteAddr,
			"RemoteIP":   GetRemoteIP(r),
		},
	})
	if err != nil {
		return err
	}
	_, err = out.WriteTo(w)
	return err
}

// 注意，相关 ip 获取未做反向代理安全判断，可能导致安全降级

func GetRemoteIP(r *http.Request) string {
	// 最先取 cloudflare 的头
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	if forwardedFor := r.Header.Get("X-Forwarded-For"); forwardedFor != "" {
		ips := strings.Split(forwardedFor, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
