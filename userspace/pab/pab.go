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
