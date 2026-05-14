package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/middleware/subscribe"
)

type eventRecorder struct {
	path       []string
	childPaths [][]string
}

func (e *eventRecorder) Child(paths ...string) subscribe.Subscriber {
	next := append(append([]string(nil), e.path...), paths...)
	e.childPaths = append(e.childPaths, next)
	return &eventRecorder{path: next, childPaths: e.childPaths}
}

func (e *eventRecorder) Publish(_ context.Context, _, _ string) error {
	return nil
}

func (e *eventRecorder) Subscribe(_ context.Context, _ string) (subscribe.Subscription, error) {
	return &noopSubscription{}, nil
}

type noopSubscription struct{}

func (n *noopSubscription) Events() <-chan subscribe.Event {
	return make(chan subscribe.Event)
}

func (n *noopSubscription) Errors() <-chan error {
	return make(chan error)
}

func (n *noopSubscription) Close() error {
	return nil
}

func TestFilterContextVersionEvent(t *testing.T) {
	recorder := &eventRecorder{path: []string{"domain", "org1", "repo1"}}
	ctx := FilterContext{
		PageContent: &PageContent{
			PageMetaContent: &PageMetaContent{CommitID: "commit-123"},
			Owner:           "org1",
			Repo:            "repo1",
		},
		VersionEvent: recorder.Child("commit-123"),
	}

	assert.NotNil(t, ctx.VersionEvent)
	assert.Len(t, recorder.childPaths, 1)
	assert.Equal(t, []string{"domain", "org1", "repo1", "commit-123"}, recorder.childPaths[0])
}

func TestFilterContextSharedEvent(t *testing.T) {
	recorder := &eventRecorder{path: []string{"domain", "org1", "repo1"}}
	ctx := FilterContext{SharedEvent: recorder.Child("shared")}

	assert.NotNil(t, ctx.SharedEvent)
	assert.Len(t, recorder.childPaths, 1)
	assert.Equal(t, []string{"domain", "org1", "repo1", "shared"}, recorder.childPaths[0])
}
