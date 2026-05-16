package goja

import (
	"errors"
	"sync"
)

type Closers struct {
	mu      sync.Mutex
	closers []func() error
}

func NewClosers() *Closers {
	return &Closers{
		closers: make([]func() error, 0),
	}
}

func (c *Closers) AddCloser(closer func() error) {
	c.mu.Lock()
	c.closers = append(c.closers, closer)
	c.mu.Unlock()
}

func (c *Closers) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var errs []error
	for i := len(c.closers) - 1; i >= 0; i-- {
		if err := c.closers[i](); err != nil {
			errs = append(errs, err)
		}
	}
	c.closers = nil
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
