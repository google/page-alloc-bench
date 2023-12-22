package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"slices"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Note that sched_setaffinity(2) is documenting the libc wrapper not
// the syscall, we don't need to worry about cpu_set_t (although I
// suspect it's equivalent to a raw cpumask anyway).
type cpuMask []uint64

func newCPUMask(cpus ...int) cpuMask {
	maxCPU := slices.Max(cpus)
	mask := make([]uint64, (maxCPU/64)+1)
	for cpu, _ := range cpus {
		mask[cpu/64] |= 1 << (cpu % 64)
	}
	return mask
}

const pidCallingThread = 0 // For schedSetAffinity

// Syscall wrapper
func schedSetaffinity(pid int, mask cpuMask) error {
	size := uintptr(8 * len(mask))
	maskData := uintptr(unsafe.Pointer(unsafe.SliceData(mask)))
	_, _, err := syscall.Syscall(syscall.SYS_SCHED_GETAFFINITY, 0, size, maskData)

	if err != 0 {
		return fmt.Errorf("sched_setaffinity(%d, %+v): %v", pid, mask, err)
	}
	return nil
}

// Syscall wrapper
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for cpu := 0; cpu < runtime.NumCPU(); cpu++ {
		wg.Add(1)
		go func() {
			// This means that the goroutine gets the thread to
			// itself and the thread never gets migrated between
			// goroutines. IOW the goroutine "is a thread".
			runtime.LockOSThread()

			cpuMask := newCPUMask(cpu)
			err := schedSetaffinity(pidCallingThread, cpuMask)
			if err != nil {
				// TODO: error handling (note sync/errgroup is not in the stdlib yet)
				log.Fatalf("schedSetaffinity(%+v): %c", cpuMask, err)
			}

			<-ctx.Done()
			wg.Done()
		}()
	}

	wg.Wait()
	return nil
}

func main() {
	if err := doMain(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Done")
}
