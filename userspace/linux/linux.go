// Package linux provides wrappers for linux syscalls.
package linux

import (
	"fmt"
	"os"
	"slices"
	"syscall"
	"unsafe"
)

// Note that sched_setaffinity(2) is documenting the libc wrapper not
// the syscall, we don't need to worry about cpu_set_t (although I
// suspect it's equivalent to a raw cpumask anyway).
type CPUMask []uint64

// NewCPUMask creates a CPUMask with the given CPU numbers set.
func NewCPUMask(cpus ...int) CPUMask {
	maxCPU := slices.Max(cpus)
	mask := make([]uint64, (maxCPU/64)+1)
	for cpu, _ := range cpus {
		mask[cpu/64] |= 1 << (cpu % 64)
	}
	return mask
}

// PIDCallingThread is an argument for SchedSetaffinity.
const PIDCallingThread = 0

// SchedSetaffinity wraps the sched_setaffinity syscall. Use PIDCallingThread
// for the pid argument to set affinity for the current thread.
func SchedSetaffinity(pid int, mask CPUMask) error {
	size := uintptr(8 * len(mask))
	maskData := uintptr(unsafe.Pointer(unsafe.SliceData(mask)))
	_, _, err := syscall.Syscall(syscall.SYS_SCHED_SETAFFINITY, 0, size, maskData)
	if err != 0 {
		return fmt.Errorf("sched_setaffinity(%d, %+v): %v", pid, mask, err)
	}

	return nil
}

// Ioctl wraps the ioctl syscall.
func Ioctl(file *os.File, cmd, arg uintptr) error {
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), cmd, arg)
	if err != 0 {
		return fmt.Errorf("ioctl 0x%x 0x%x on %s: %v\n", cmd, arg, file.Name(), err)
	}
	return nil
}

// For debugging. SYS_GETCPU not in the syscall package. So this only workds on amd64.
func getcpu() (int, error) {
	var cpu int
	_, _, err := syscall.Syscall(309, uintptr(unsafe.Pointer(&cpu)), 0, 0)
	if err != 0 {
		return -1, fmt.Errorf("getcpu: %v", err)
	}
	return cpu, nil
}
