package goja

import (
	"testing"

	"github.com/dop251/goja"
	"github.com/stretchr/testify/assert"
)

func TestBodyBytesFromValueUsesTypedArrayViewWindow(t *testing.T) {
	vm := goja.New()
	value, err := vm.RunString(`new Uint8Array([10, 20, 30, 40]).subarray(1, 3)`)
	assert.NoError(t, err)

	body, err := bodyBytesFromValue(vm, value)
	assert.NoError(t, err)
	assert.Equal(t, []byte{20, 30}, body)
}
