package utils

import (
	"io"
)

type SizeReadCloser struct {
	io.ReadCloser
	Size uint64
}

type CloserWrapper struct {
	io.ReadCloser
	OnClose func()
}

func (c *CloserWrapper) Close() error {
	defer c.OnClose()
	return c.ReadCloser.Close()
}
