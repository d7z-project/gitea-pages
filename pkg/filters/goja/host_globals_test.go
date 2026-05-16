package goja

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/middleware/subscribe"
)

type testEventSubscriber struct {
	mu    sync.Mutex
	subs  []*testEventSubscription
	calls int
}

func (t *testEventSubscriber) Publish(context.Context, string, string) error {
	return nil
}

func (t *testEventSubscriber) Child(paths ...string) subscribe.Subscriber {
	return t
}

func (t *testEventSubscriber) Subscribe(ctx context.Context, key string) (subscribe.Subscription, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls++
	sub := &testEventSubscription{
		events: make(chan subscribe.Event, 8),
		errors: make(chan error, 8),
		done:   make(chan struct{}),
	}
	t.subs = append(t.subs, sub)
	go func() {
		<-ctx.Done()
		_ = sub.Close()
	}()
	return sub, nil
}

func (t *testEventSubscriber) current() *testEventSubscription {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.subs) == 0 {
		return nil
	}
	return t.subs[len(t.subs)-1]
}

func (t *testEventSubscriber) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

type testEventSubscription struct {
	events chan subscribe.Event
	errors chan error
	done   chan struct{}
	once   sync.Once
}

func (t *testEventSubscription) Events() <-chan subscribe.Event { return t.events }
func (t *testEventSubscription) Errors() <-chan error           { return t.errors }
func (t *testEventSubscription) Close() error {
	t.once.Do(func() {
		close(t.done)
		close(t.events)
		close(t.errors)
	})
	return nil
}

func TestEventStreamResubscribesAfterSubscriptionError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	subscriber := &testEventSubscriber{}
	stream := &eventStream{limit: 8}
	streams := map[string]*eventStream{"topic": stream}
	var streamsMu sync.Mutex
	firstSub, err := subscriber.Subscribe(ctx, "topic")
	assert.NoError(t, err)

	go superviseEventStream(ctx, "topic", &streamsMu, streams, subscriber.Subscribe, stream, firstSub)

	assert.Eventually(t, func() bool { return subscriber.count() == 1 }, time.Second, 10*time.Millisecond)
	first := subscriber.current()
	first.errors <- errors.New("temporary")

	assert.Eventually(t, func() bool { return subscriber.count() == 2 }, time.Second, 10*time.Millisecond)
	second := subscriber.current()

	resultCh := make(chan eventResult, 1)
	waiter := make(chan eventResult, 1)
	stream.mu.Lock()
	stream.waiters = append(stream.waiters, waiter)
	stream.mu.Unlock()

	go func() {
		resultCh <- <-waiter
	}()
	second.events <- subscribe.Event{Value: "ok"}

	select {
	case result := <-resultCh:
		assert.NoError(t, result.err)
		assert.Equal(t, "ok", result.value)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for resubscribed event")
	}
}
