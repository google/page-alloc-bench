// Copyright 2024 Google LLC
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of the GNU General Public License
// as published by the Free Software Foundation; either version 2
// of the License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program; If not, see <http://www.gnu.org/licenses/>.

// Package pab contains global utilities for page_alloc_bench.
package pab

import (
	"fmt"
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

func (s ByteSize) String() string {
	switch {
	case s < Megabyte:
		return fmt.Sprintf("%dB")
	case s < Gigabyte:
		return fmt.Sprintf("%.2fMiB", float64(s)/float64(Megabyte))
	default:
		return fmt.Sprintf("%.2fGiB", float64(s)/float64(Gigabyte))
	}
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
