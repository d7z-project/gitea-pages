package utils

import "sync"

type Locker struct {
	sy sync.Map
}

func NewLocker() *Locker {
	return &Locker{
		sy: sync.Map{},
	}
}
func (l *Locker) Open(key string) *sync.Mutex {
	actual, _ := l.sy.LoadOrStore(key, &sync.Mutex{})
	return actual.(*sync.Mutex)
}
