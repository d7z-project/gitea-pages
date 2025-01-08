package core

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAny(t *testing.T) {
	data := make(map[string]string)
	err := json.Unmarshal([]byte(`{}`), &data)
	require.NoError(t, err)
}
