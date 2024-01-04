// Package kmod interacts with this benchmarks' special kernel module.
package kmod

import (
	"os"
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

// Page is an opaque ID for a page.
type Page uintptr

// AllocPage allocates a page.
func (k *Connection) AllocPage() (Page, error) {
	var page Page
	err := linux.Ioctl(k.File, C.pab_ioctl_alloc_page, uintptr(unsafe.Pointer(&page)))
	return page, err
}

// FreePage frees a page.
func (k *Connection) FreePage(page Page) error {
	return linux.Ioctl(k.File, C.pab_ioctl_free_page, uintptr(page))
}
