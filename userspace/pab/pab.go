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
	"runtime"

	"github.com/google/page_alloc_bench/linux"
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
	abs := s
	if s < 0 {
		abs = -s
	}
	switch {
	case abs < Kilobyte:
		return fmt.Sprintf("%dB", s)
	case abs < Megabyte:
		return fmt.Sprintf("%.2fKiB", float64(s)/float64(Kilobyte))
	case abs < Gigabyte:
		return fmt.Sprintf("%.2fMiB", float64(s)/float64(Megabyte))
	default:
		return fmt.Sprintf("%.2fGiB", float64(s)/float64(Gigabyte))
	}
}

func (s ByteSize) Mul(x int) ByteSize {
	return ByteSize(int64(x) * int64(s))
}

// Lock the calling goroutine to the current CPU. Child goroutines are
// unaffected.
func LockGoroutineToCPU(cpu int) error {
	// This means that the goroutine gets the thread to itself and never
	// migrates to another thread. Basically the goroutine "is a thread"
	// now.
	runtime.LockOSThread()

	cpuMask := linux.NewCPUMask(cpu)
	err := linux.SchedSetaffinity(linux.PIDCallingThread, cpuMask)
	if err != nil {
		return fmt.Errorf("SchedSetaffinity(%+v): %c", cpuMask, err)
	}

	return nil
}
