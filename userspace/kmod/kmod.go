// Package kmod interacts with this benchmarks' special kernel module.
package kmod

import (
	"os"
	"unsafe"

	"github.com/google/page_alloc_bench/linux"
)

const (
	// Needs to be manually synced with the C file
	PAB_IOCTL_ALLOC_PAGE = 0x12340001
	PAB_IOCTL_FREE_PAGE  = 0x12340002
)

// Connection is a connection to a loaded kernel module.
type Connection struct {
	*os.File
}

// Page is an opaque ID for a page.
type Page uintptr

// AllocPage allocates a page.
func (k *Connection) AllocPage() (Page, error) {
	var page Page
	err := linux.Ioctl(k.File, PAB_IOCTL_ALLOC_PAGE, uintptr(unsafe.Pointer(&page)))
	return page, err
}

// FreePage frees a page.
func (k *Connection) FreePage(page Page) error {
	return linux.Ioctl(k.File, PAB_IOCTL_FREE_PAGE, uintptr(page))
}
