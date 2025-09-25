package cache

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCacheGetPutDelete(t *testing.T) {
	memory := NewCacheMemory(1024, 10240)

	require.NoError(t, memory.Put("hello", strings.NewReader("world")))

	value, err := memory.Get("hello")
	require.NoError(t, err)
	all, err := io.ReadAll(value)
	require.NoError(t, err)
	require.Equal(t, "world", string(all))
	require.Equal(t, 5, memory.current)

	require.NoError(t, memory.Put("hello", strings.NewReader("kotlin")))

	value, err = memory.Get("hello")
	require.NoError(t, err)
	all, err = io.ReadAll(value)
	require.NoError(t, err)
	require.Equal(t, "kotlin", string(all))
	require.Equal(t, 6, memory.current)
	require.Equal(t, 1, len(memory.data))

	require.NoError(t, memory.Put("data", strings.NewReader("kotlin")))
	require.Equal(t, 12, memory.current)
	require.Equal(t, 2, len(memory.data))
	require.Equal(t, 2, len(memory.ordered))

	require.NoError(t, memory.Delete("hello"))
	value, err = memory.Get("hello")
	require.Error(t, err)
	require.Equal(t, 1, len(memory.data))
	require.Equal(t, 1, len(memory.ordered))

	require.NoError(t, memory.Put("hello", nil))
	value, err = memory.Get("hello")
	require.NoError(t, err)
	require.Nil(t, value)
}

func TestCacheLimit(t *testing.T) {
	memory := NewCacheMemory(5, 5*5)
	require.NoError(t, memory.Put("hello", strings.NewReader("world")))
	require.Equal(t, 5, memory.current)
	require.ErrorIs(t, memory.Put("hello", strings.NewReader("world1")), ErrCacheOutOfMemory)
	require.Equal(t, 5, memory.current)
	for i := 0; i < 4; i++ {
		require.NoError(t, memory.Put(fmt.Sprintf("hello-%d", i), strings.NewReader("govet")))
	}
	value, err := memory.Get("hello")
	require.NoError(t, err)
	all, err := io.ReadAll(value)
	require.NoError(t, err)
	require.Equal(t, "world", string(all))

	require.NoError(t, memory.Put("test", strings.NewReader("govet")))

	value, err = memory.Get("hello")
	require.ErrorIs(t, err, os.ErrNotExist)

	require.Equal(t, 5, len(memory.data))
	require.Equal(t, 5, len(memory.ordered))
	require.Equal(t, 5, len(memory.lastModify))
}
