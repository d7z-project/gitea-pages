package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestPageConfigRouteRejectsInvalidPathTypes(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{
			name: "array",
			data: `
routes:
  - path: ["/api/**"]
    js: {}
`,
		},
		{
			name: "number",
			data: `
routes:
  - path: 42
    js: {}
`,
		},
		{
			name: "object",
			data: `
routes:
  - path:
      value: "/api/**"
    js: {}
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg PageConfig
			err := yaml.Unmarshal([]byte(tc.data), &cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "route path must be a string")
		})
	}
}

func TestPageConfigRouteRejectsMissingOrAmbiguousFields(t *testing.T) {
	for _, tc := range []struct {
		name    string
		data    string
		message string
	}{
		{
			name: "missing path",
			data: `
routes:
  - js: {}
`,
			message: "missing path field",
		},
		{
			name: "empty path",
			data: `
routes:
  - path: ""
    js: {}
`,
			message: "route path cannot be empty",
		},
		{
			name: "multiple filters",
			data: `
routes:
  - path: "/api/**"
    js: {}
    direct: {}
`,
			message: "route must define exactly one filter",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg PageConfig
			err := yaml.Unmarshal([]byte(tc.data), &cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.message)
		})
	}
}

func TestPageConfigRouteKeepsEmptyParamsCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{
			name: "null params",
			data: `
routes:
  - path: "/api/**"
    reverse_proxy:
`,
		},
		{
			name: "string params",
			data: `
routes:
  - path: "/api/**"
    reverse_proxy: "compat"
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg PageConfig
			require.NoError(t, yaml.Unmarshal([]byte(tc.data), &cfg))
			require.Len(t, cfg.Routes, 1)
			assert.Equal(t, "/api/**", cfg.Routes[0].Path)
			assert.Equal(t, "reverse_proxy", cfg.Routes[0].Type)
			assert.Empty(t, cfg.Routes[0].Params)
		})
	}
}
