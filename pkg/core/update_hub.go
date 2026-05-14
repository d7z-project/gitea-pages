package core

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"gopkg.d7z.net/middleware/subscribe"
)

type RepoUpdateHub struct {
	event subscribe.Subscriber

	mu    sync.Mutex
	repos map[string]*repoUpdateGroup
}

type repoUpdateGroup struct {
	owner string
	repo  string

	sub       subscribe.Subscription
	done      chan struct{}
	closeOnce sync.Once

	mu             sync.Mutex
	closed         bool
	lastSeenCommit string
	watchers       map[string]map[string]*requestWatcher
}

type requestWatcher struct {
	kill func()
	once sync.Once
}

func NewRepoUpdateHub(event subscribe.Subscriber) *RepoUpdateHub {
	return &RepoUpdateHub{
		event: event,
		repos: make(map[string]*repoUpdateGroup),
	}
}

func (h *RepoUpdateHub) PublishUpdate(ctx context.Context, owner, repo, commitID string) error {
	if h == nil || h.event == nil {
		return nil
	}
	return h.event.Child("system", owner, repo).Publish(ctx, "_update", commitID)
}

func (h *RepoUpdateHub) Attach(owner, repo, commitID, requestID string, kill func()) (func(), error) {
	if h == nil || h.event == nil || kill == nil {
		return func() {}, nil
	}
	key := fmt.Sprintf("%s/%s", owner, repo)

	h.mu.Lock()
	group, ok := h.repos[key]
	if !ok {
		sub, err := h.event.Child("system", owner, repo).Subscribe(context.Background(), "_update")
		if err != nil {
			h.mu.Unlock()
			return nil, err
		}
		group = &repoUpdateGroup{
			owner:    owner,
			repo:     repo,
			sub:      sub,
			done:     make(chan struct{}),
			watchers: make(map[string]map[string]*requestWatcher),
		}
		h.repos[key] = group
		go h.runGroup(key, group)
	}

	group.mu.Lock()
	if group.closed {
		group.mu.Unlock()
		h.mu.Unlock()
		return h.Attach(owner, repo, commitID, requestID, kill)
	}
	if group.watchers[commitID] == nil {
		group.watchers[commitID] = make(map[string]*requestWatcher)
	}
	group.watchers[commitID][requestID] = &requestWatcher{kill: kill}
	group.mu.Unlock()
	h.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			h.detach(key, group, commitID, requestID)
		})
	}, nil
}

func (h *RepoUpdateHub) detach(key string, group *repoUpdateGroup, commitID, requestID string) {
	h.mu.Lock()
	group.mu.Lock()
	bucket := group.watchers[commitID]
	if bucket != nil {
		delete(bucket, requestID)
		if len(bucket) == 0 {
			delete(group.watchers, commitID)
		}
	}
	shouldClose := len(group.watchers) == 0 && !group.closed
	if shouldClose {
		group.closed = true
		delete(h.repos, key)
	}
	group.mu.Unlock()
	h.mu.Unlock()

	if shouldClose {
		group.close()
	}
}

func (h *RepoUpdateHub) runGroup(key string, group *repoUpdateGroup) {
	events := group.sub.Events()
	errors := group.sub.Errors()
	for events != nil || errors != nil {
		select {
		case <-group.done:
			return
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			h.handleUpdate(group, event.Value)
		case err, ok := <-errors:
			if !ok {
				errors = nil
				continue
			}
			slog.Warn("repo update watcher error", "owner", group.owner, "repo", group.repo, "error", err)
		}
	}
	h.mu.Lock()
	group.mu.Lock()
	if !group.closed {
		group.closed = true
		if current, ok := h.repos[key]; ok && current == group {
			delete(h.repos, key)
		}
	}
	group.mu.Unlock()
	h.mu.Unlock()
	group.close()
}

func (h *RepoUpdateHub) handleUpdate(group *repoUpdateGroup, commitID string) {
	group.mu.Lock()
	if commitID == group.lastSeenCommit {
		group.mu.Unlock()
		return
	}
	group.lastSeenCommit = commitID
	victims := make([]*requestWatcher, 0)
	for bucketCommit, bucket := range group.watchers {
		if bucketCommit == commitID {
			continue
		}
		for _, watcher := range bucket {
			victims = append(victims, watcher)
		}
		delete(group.watchers, bucketCommit)
	}
	group.mu.Unlock()

	for _, watcher := range victims {
		watcher.once.Do(watcher.kill)
	}
}

func (g *repoUpdateGroup) close() {
	g.closeOnce.Do(func() {
		close(g.done)
		_ = g.sub.Close()
	})
}
