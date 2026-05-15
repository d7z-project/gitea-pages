package core

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
)

type PageSecurity struct {
	CORS    PageCORSConfig   `yaml:"cors" json:"cors"`
	Cookies PageCookieConfig `yaml:"cookies" json:"cookies"`
	Headers PageHeaderConfig `yaml:"headers" json:"headers"`
}

type PageCORSConfig struct {
	Origins     []string `yaml:"origins" json:"origins"`
	Methods     []string `yaml:"methods" json:"methods"`
	Headers     []string `yaml:"headers" json:"headers"`
	Expose      []string `yaml:"expose" json:"expose"`
	Credentials bool     `yaml:"credentials" json:"credentials"`
	MaxAge      int      `yaml:"max_age" json:"max_age"`
}

type PageCookieConfig struct {
	Enabled          bool `yaml:"enabled" json:"enabled"`
	RequireHTTPS     bool `yaml:"require_https" json:"require_https"`
	AllowCrossOrigin bool `yaml:"allow_cross_origin" json:"allow_cross_origin"`
}

type PageHeaderConfig struct {
	CrossOriginResourcePolicy string `yaml:"cross_origin_resource_policy" json:"cross_origin_resource_policy"`
	FrameOptions              string `yaml:"frame_options" json:"frame_options"`
}

type SecurityResult struct {
	AllowedOrigin        string
	AllowCredentials     bool
	AllowMethods         string
	AllowHeaders         string
	ExposeHeaders        string
	MaxAge               int
	AllowRequestCookies  bool
	AllowResponseCookies bool
	AllowWebSocket       bool
	IsPreflight          bool
	ResourcePolicy       string
	FrameOptions         string
}

func DefaultPageSecurity() PageSecurity {
	return PageSecurity{
		CORS: PageCORSConfig{
			Methods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			Headers: []string{"content-type", "authorization"},
			MaxAge:  600,
		},
		Cookies: PageCookieConfig{
			Enabled:      true,
			RequireHTTPS: true,
		},
		Headers: PageHeaderConfig{
			CrossOriginResourcePolicy: "same-origin",
		},
	}
}

func ApplyPageSecurityDefaults(security *PageSecurity) {
	if security == nil {
		return
	}
	defaults := DefaultPageSecurity()
	if len(security.CORS.Methods) == 0 {
		security.CORS.Methods = append([]string(nil), defaults.CORS.Methods...)
	}
	if len(security.CORS.Headers) == 0 {
		security.CORS.Headers = append([]string(nil), defaults.CORS.Headers...)
	}
	if security.CORS.MaxAge <= 0 {
		security.CORS.MaxAge = defaults.CORS.MaxAge
	}
	if security.Headers.CrossOriginResourcePolicy == "" {
		security.Headers.CrossOriginResourcePolicy = defaults.Headers.CrossOriginResourcePolicy
	}
}

func BuildSecurityResult(r *http.Request, info RequestInfo, security PageSecurity) SecurityResult {
	ApplyPageSecurityDefaults(&security)
	result := SecurityResult{
		AllowMethods:   strings.Join(security.CORS.Methods, ", "),
		AllowHeaders:   strings.Join(security.CORS.Headers, ", "),
		ExposeHeaders:  strings.Join(security.CORS.Expose, ", "),
		MaxAge:         security.CORS.MaxAge,
		ResourcePolicy: security.Headers.CrossOriginResourcePolicy,
		FrameOptions:   security.Headers.FrameOptions,
	}
	if r == nil {
		return result
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	sameOrigin := sameOriginRequest(origin, info)
	crossOriginAllowed := origin != "" && (sameOrigin || slices.Contains(security.CORS.Origins, origin))
	if crossOriginAllowed {
		result.AllowedOrigin = origin
	}
	result.AllowCredentials = crossOriginAllowed && security.CORS.Credentials
	result.IsPreflight = r.Method == http.MethodOptions &&
		origin != "" &&
		strings.TrimSpace(r.Header.Get("Access-Control-Request-Method")) != ""

	requestCookiesAllowed := security.Cookies.Enabled
	if security.Cookies.RequireHTTPS && info.Scheme != "https" {
		requestCookiesAllowed = false
	}
	if origin != "" && !sameOrigin && !security.Cookies.AllowCrossOrigin {
		requestCookiesAllowed = false
	}
	responseCookiesAllowed := requestCookiesAllowed
	if origin != "" && !sameOrigin && !security.CORS.Credentials {
		responseCookiesAllowed = false
	}

	result.AllowRequestCookies = requestCookiesAllowed
	result.AllowResponseCookies = responseCookiesAllowed
	result.AllowWebSocket = origin == "" || crossOriginAllowed
	return result
}

func sameOriginRequest(origin string, info RequestInfo) bool {
	if origin == "" || info.Host == "" || info.Scheme == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if !strings.EqualFold(parsed.Scheme, info.Scheme) {
		return false
	}
	return strings.EqualFold(parsed.Host, info.Host)
}
