package utils

import "io"

type SizeReadCloser struct {
	io.ReadCloser
	Size uint64
}
