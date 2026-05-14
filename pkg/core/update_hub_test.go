package core

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.d7z.net/middleware/subscribe"
)

type countingSubscriber struct {
	subscribe.Subscriber
	subscribeCalls *atomic.Int32
}

func (c *countingSubscriber) Child(paths ...string) subscribe.Subscriber {
	child := c.Subscriber.Child(paths...)
	return &countingSubscriber{Subscriber: child, subscribeCalls: c.subscribeCalls}
}

func (c *countingSubscriber) Subscribe(ctx context.Context, key string) (subscribe.Subscription, error) {
	c.subscribeCalls.Add(1)
	return c.Subscriber.Subscribe(ctx, key)
}

type closingSubscriber struct {
	subscribeCalls atomic.Int32
}

func (c *closingSubscriber) Child(paths ...string) subscribe.Subscriber {
	return c
}

func (c *closingSubscriber) Publish(ctx context.Context, key, data string) error {
	return nil
}

func (c *closingSubscriber) Subscribe(ctx context.Context, key string) (subscribe.Subscription, error) {
	c.subscribeCalls.Add(1)
	events := make(chan subscribe.Event)
	errors := make(chan error)
	close(events)
	close(errors)
	return &testSubscription{events: events, errors: errors}, nil
}

type testSubscription struct {
	events chan subscribe.Event
	errors chan error
}

func (t *testSubscription) Events() <-chan subscribe.Event { return t.events }
func (t *testSubscription) Errors() <-chan error           { return t.errors }
func (t *testSubscription) Close() error                   { return nil }

func TestRepoUpdateHubKillsOlderCommitsOnly(t *testing.T) {
	hub := NewRepoUpdateHub(subscribe.NewMemorySubscriber())
	var oldKilled atomic.Int32
	var newKilled atomic.Int32

	releaseOld, err := hub.Attach("org1", "repo1", "old", "req-old", func() { oldKilled.Add(1) })
	require.NoError(t, err)
	defer releaseOld()
	releaseNew, err := hub.Attach("org1", "repo1", "new", "req-new", func() { newKilled.Add(1) })
	require.NoError(t, err)
	defer releaseNew()

	require.NoError(t, hub.PublishUpdate(context.Background(), "org1", "repo1", "new"))

	assert.Eventually(t, func() bool { return oldKilled.Load() == 1 }, time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(0), newKilled.Load())
}

func TestRepoUpdateHubReusesSingleSubscriptionPerRepo(t *testing.T) {
	counter := &atomic.Int32{}
	base := &countingSubscriber{Subscriber: subscribe.NewMemorySubscriber(), subscribeCalls: counter}
	hub := NewRepoUpdateHub(base)

	releaseA, err := hub.Attach("org1", "repo1", "a", "req-a", func() {})
	require.NoError(t, err)
	defer releaseA()
	releaseB, err := hub.Attach("org1", "repo1", "b", "req-b", func() {})
	require.NoError(t, err)
	defer releaseB()

	assert.Equal(t, int32(1), counter.Load())
}

func TestRepoUpdateHubReleaseIsIdempotent(t *testing.T) {
	hub := NewRepoUpdateHub(subscribe.NewMemorySubscriber())
	var killed atomic.Int32

	release, err := hub.Attach("org1", "repo1", "old", "req-old", func() { killed.Add(1) })
	require.NoError(t, err)

	release()
	release()

	require.NoError(t, hub.PublishUpdate(context.Background(), "org1", "repo1", "new"))
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(0), killed.Load())
}

func TestRepoUpdateHubRecreatesSubscriptionAfterRepoBecomesIdle(t *testing.T) {
	counter := &atomic.Int32{}
	base := &countingSubscriber{Subscriber: subscribe.NewMemorySubscriber(), subscribeCalls: counter}
	hub := NewRepoUpdateHub(base)

	releaseA, err := hub.Attach("org1", "repo1", "a", "req-a", func() {})
	require.NoError(t, err)
	releaseA()

	releaseB, err := hub.Attach("org1", "repo1", "b", "req-b", func() {})
	require.NoError(t, err)
	defer releaseB()

	assert.Equal(t, int32(2), counter.Load())
}

func TestRepoUpdateHubKillIsIdempotentAcrossRepeatedUpdates(t *testing.T) {
	hub := NewRepoUpdateHub(subscribe.NewMemorySubscriber())
	var killed atomic.Int32

	release, err := hub.Attach("org1", "repo1", "old", "req-old", func() { killed.Add(1) })
	require.NoError(t, err)
	defer release()

	require.NoError(t, hub.PublishUpdate(context.Background(), "org1", "repo1", "new"))
	require.NoError(t, hub.PublishUpdate(context.Background(), "org1", "repo1", "new"))

	assert.Eventually(t, func() bool { return killed.Load() == 1 }, time.Second, 10*time.Millisecond)
}

func TestRepoUpdateHubUsesInternalSystemNamespace(t *testing.T) {
	counter := &atomic.Int32{}
	base := &countingSubscriber{Subscriber: subscribe.NewMemorySubscriber(), subscribeCalls: counter}
	hub := NewRepoUpdateHub(base)

	require.NoError(t, hub.PublishUpdate(context.Background(), "org1", "repo1", "abc123"))

	stream, err := base.Subscriber.Child("shared", "org1", "repo1").Subscribe(context.Background(), "_update")
	require.NoError(t, err)
	defer stream.Close()

	select {
	case <-stream.Events():
		t.Fatal("shared namespace unexpectedly received internal update event")
	case <-time.After(50 * time.Millisecond):
	}

	internal, err := base.Subscriber.Child("system", "org1", "repo1").Subscribe(context.Background(), "_update")
	require.NoError(t, err)
	defer internal.Close()
	require.NoError(t, hub.PublishUpdate(context.Background(), "org1", "repo1", "def456"))
	assert.Eventually(t, func() bool {
		select {
		case evt := <-internal.Events():
			return evt.Value == "def456"
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
}

func TestRepoUpdateHubReattachesAfterWatcherStops(t *testing.T) {
	base := &closingSubscriber{}
	hub := NewRepoUpdateHub(base)

	releaseA, err := hub.Attach("org1", "repo1", "a", "req-a", func() {})
	require.NoError(t, err)
	defer releaseA()

	assert.Eventually(t, func() bool { return base.subscribeCalls.Load() == 1 }, time.Second, 10*time.Millisecond)
	assert.Eventually(t, func() bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		_, ok := hub.repos["org1/repo1"]
		return !ok
	}, time.Second, 10*time.Millisecond)

	releaseB, err := hub.Attach("org1", "repo1", "b", "req-b", func() {})
	require.NoError(t, err)
	defer releaseB()

	assert.Eventually(t, func() bool { return base.subscribeCalls.Load() == 2 }, time.Second, 10*time.Millisecond)
}
