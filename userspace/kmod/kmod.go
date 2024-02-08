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

// Package kmod interacts with this benchmarks' special kernel module.
package kmod

import (
	"os"
	"time"
	"unsafe"

	"github.com/google/page_alloc_bench/linux"
)

/*
#include <stdint.h>

#include "page_alloc_bench.h"

const uintptr_t pab_ioctl_alloc_page = PAB_IOCTL_ALLOC_PAGE;
const uintptr_t pab_ioctl_free_page = PAB_IOCTL_FREE_PAGE;
*/
import "C"

// Connection is a connection to a loaded kernel module.
type Connection struct {
	*os.File
}

// Page represents a page allocated by the kernel module.
type Page struct {
	NID     int           // NUMA node ID
	Latency time.Duration // Excluding syscall/userspace overhead.
	id      C.ulong       // Opaque ID (spoiler: struct page *) used to free it.
}

// AllocPage allocates a page. Returned errors will wrap a syscall.Errno where
// possible.
func (k *Connection) AllocPage(order int) (*Page, error) {
	var ioctl C.struct_pab_ioctl_alloc_page
	ioctl.args.order = C.int(order)
	err := linux.Ioctl(k.File, C.pab_ioctl_alloc_page, uintptr(unsafe.Pointer(&ioctl)))
	p := Page{
		id:      ioctl.result.id,
		Latency: time.Duration(ioctl.result.latency_ns) * time.Nanosecond,
		NID:     int(ioctl.result.nid),
	}
	return &p, err
}

// FreePage frees a page.
func (k *Connection) FreePage(page *Page) error {
	return linux.Ioctl(k.File, C.pab_ioctl_free_page, uintptr(page.id))
}
