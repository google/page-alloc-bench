package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

func ioctl(file *os.File, cmd, arg uintptr) error {
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), cmd, arg)
	if err != 0 {
		return fmt.Errorf("ioctl 0x%x 0x%x on %s: %v\n", cmd, arg, file.Name(), err)
	}
	return nil
}

const (
	// Needs to be manually synced with the C file
	PAB_IOCTL_ALLOC_PAGE = 0x12340001
	PAB_IOCTL_FREE_PAGE  = 0x12340002
)

type kmod struct {
	*os.File
}

// Opaque ID for a page.
type page uintptr

func (k *kmod) allocPage() (page, error) {
	var page page
	err := ioctl(k.File, PAB_IOCTL_ALLOC_PAGE, uintptr(unsafe.Pointer(&page)))
	return page, err
}

func (k *kmod) freePage(page page) error {
	return ioctl(k.File, PAB_IOCTL_FREE_PAGE, uintptr(page))
}

func doMain() error {
	file, err := os.Open("/proc/page_alloc_bench")
	if err != nil {
		return fmt.Errorf("Opening /proc/page_alloc_bench: %v", err)
	}
	kmod := kmod{file}
	defer kmod.Close()

	page, err := kmod.allocPage()
	if err != nil {
		return err
	}
	fmt.Printf("%x\n", page)
	defer kmod.freePage(page)
	return nil
}

func main() {
	if err := doMain(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
