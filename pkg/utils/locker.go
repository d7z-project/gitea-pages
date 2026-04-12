package utils

import "sync"

type Locker struct {
	mu sync.Mutex
	sy map[string]*lockerEntry
}

type lockerEntry struct {
	mu   sync.Mutex
	refs int
}

type KeyLock struct {
	parent   *Locker
	key      string
	entry    *lockerEntry
	locked   bool
	released bool
}

func NewLocker() *Locker {
	return &Locker{
		sy: make(map[string]*lockerEntry),
	}
}

func (l *Locker) Open(key string) *KeyLock {
	l.mu.Lock()
	entry, ok := l.sy[key]
	if !ok {
		entry = &lockerEntry{}
		l.sy[key] = entry
	}
	entry.refs++
	l.mu.Unlock()

	return &KeyLock{
		parent: l,
		key:    key,
		entry:  entry,
	}
}

func (k *KeyLock) Lock() {
	if k.locked {
		return
	}
	k.entry.mu.Lock()
	k.locked = true
}

func (k *KeyLock) TryLock() bool {
	if k.locked {
		return true
	}
	if !k.entry.mu.TryLock() {
		return false
	}
	k.locked = true
	return true
}

func (k *KeyLock) Unlock() {
	if !k.locked {
		return
	}
	k.locked = false
	k.entry.mu.Unlock()
	k.release()
}

func (k *KeyLock) release() {
	if k.released {
		return
	}
	k.released = true

	k.parent.mu.Lock()
	defer k.parent.mu.Unlock()

	entry, ok := k.parent.sy[k.key]
	if !ok || entry != k.entry {
		return
	}
	entry.refs--
	if entry.refs == 0 {
		delete(k.parent.sy, k.key)
	}
}
