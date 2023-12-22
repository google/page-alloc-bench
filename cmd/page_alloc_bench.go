package main

import (
	"fmt"
	"os"
	"syscall"
)

func ioctl(file *os.File, cmd, arg uintptr) error {
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), cmd, arg)
	if err != 0 {
		return fmt.Errorf("ioctl 0x%x 0x%x on %s: %v\n", cmd, arg, file.Name(), err)
	}
	return nil
}

func doMain() error {
	file, err := os.Open("/proc/page_alloc_bench")
	if err != nil {
		return fmt.Errorf("Opening /proc/page_alloc_bench: %v", err)
	}
	defer file.Close()
	return ioctl(file, 1, 2)
}

func main() {
	if err := doMain(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
