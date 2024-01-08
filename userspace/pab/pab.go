// Package pab contains global utilities for page_alloc_bench.
package pab

import (
	"os"
	"slices"
)

const (
	Kilobyte ByteSize = 1024
	Megabyte ByteSize = 1024 * Kilobyte
	Gigabyte ByteSize = 1024 * Megabyte
)

type ByteSize int64

func (s ByteSize) Bytes() int64 {
	return int64(s)
}

func (s ByteSize) Pages() int64 {
	return s.Bytes() / int64(os.Getpagesize())
}

// Cleanups provides functionality like testing.T.Cleanup.
type Cleanups struct {
	funcs []func()
}

// Cleanup adds a cleanup.
func (c *Cleanups) Cleanup(f func()) {
	c.funcs = append(c.funcs, f)
}

// Run runs the cleanups in the reverse order of the Cleanup() calls.
func (c *Cleanups) Run() {
	slices.Reverse(c.funcs)
	for _, f := range c.funcs {
		f()
	}
	c.funcs = nil
}
