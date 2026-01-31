package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/middleware/kv"
)

func TestCNAMERegex(t *testing.T) {
	db, _ := kv.NewMemory("")
	meta := core.NewServerMeta(nil, nil, "example.com", nil, db, 0, 0, 0)

	tests := []struct {
		domain string
		valid  bool
	}{
		{"a.com", true},
		{"sub.a.com", true},
		{"a.b.c.d.com", true},
		{"invalid_name.com", false},
		{"-start.com", false},
		{"end-.com", false},
		{"a.com-too-long-tld-xxxxxxxxxxxxxxxxxxx", false},
	}

	for _, tt := range tests {
		_, ok := meta.AliasCheck(tt.domain)
		assert.Equal(t, tt.valid, ok, "Testing domain: %s", tt.domain)
	}
}

func TestAliasBindCAS(t *testing.T) {
	db, _ := kv.NewMemory("")

	alias := core.NewDomainAlias(db)

	ctx := context.Background()

	err := alias.Bind(ctx, []string{"a.com", "b.com"}, "owner1", "repo1")

	assert.NoError(t, err)

	a, err := alias.Query(ctx, "a.com")

	assert.NoError(t, err)

	if assert.NotNil(t, a) {
		assert.Equal(t, "owner1", a.Owner)
	}

	err = alias.Bind(ctx, []string{"b.com", "c.com"}, "owner1", "repo1")

	assert.NoError(t, err)

	_, err = alias.Query(ctx, "a.com")

	assert.Error(t, err)

	a, err = alias.Query(ctx, "c.com")

	assert.NoError(t, err)

	if assert.NotNil(t, a) {
		assert.Equal(t, "owner1", a.Owner)
	}
}
