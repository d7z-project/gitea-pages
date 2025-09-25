package utils

import "io"

type NopCloser struct {
	io.ReadSeeker
}

func (NopCloser) Close() error { return nil }
