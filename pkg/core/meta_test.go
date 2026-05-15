package core

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.d7z.net/middleware/kv"
)

type panicMetaBackend struct{}

func (panicMetaBackend) Close() error { return nil }

func (panicMetaBackend) Meta(context.Context, string, string) (*Metadata, error) {
	panic("boom")
}

func (panicMetaBackend) Open(context.Context, string, string, string, string, http.Header) (*http.Response, error) {
	return nil, nil
}

func (panicMetaBackend) List(context.Context, string, string, string, string) ([]DirEntry, error) {
	return nil, nil
}

func TestRunMetaUpdateRecoversFromPanic(t *testing.T) {
	store, err := kv.NewMemory("")
	require.NoError(t, err)

	meta := NewServerMeta(
		http.DefaultClient,
		panicMetaBackend{},
		"example.com",
		NewDomainAlias(store.Child("alias")),
		store.Child("cache"),
		time.Minute,
		time.Second,
		1,
		nil,
		nil,
	)

	update := &metaUpdate{done: make(chan struct{})}
	meta.updates["org/repo"] = update
	meta.runMetaUpdate("org", "repo", update)

	select {
	case <-update.done:
	default:
		t.Fatal("meta update did not close done channel")
	}

	require.Error(t, update.err)
	assert.Contains(t, update.err.Error(), "panic while refreshing page metadata")
	assert.Nil(t, update.meta)
	_, exists := meta.updates["org/repo"]
	assert.False(t, exists)
}
