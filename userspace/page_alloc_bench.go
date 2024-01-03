// Copyright 2023 Google LLC
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
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/google/page_alloc_bench/linux"
)

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
	err := linux.Ioctl(k.File, PAB_IOCTL_ALLOC_PAGE, uintptr(unsafe.Pointer(&page)))
	return page, err
}

func (k *kmod) freePage(page page) error {
	return linux.Ioctl(k.File, PAB_IOCTL_FREE_PAGE, uintptr(page))
}

const (
	kilobyte = 1024
	megabyte = 1024 * kilobyte
	gigabyte = 1024 * megabyte
)

type stats struct {
	pagesAllocated atomic.Uint64 // Only incremented; subtract pagesFreed to count leaks.
	pagesFreed     atomic.Uint64
}

func (s *stats) String() string {
	return fmt.Sprintf("allocated=%d freed=%d", s.pagesAllocated.Load(), s.pagesFreed.Load())
}

var (
	totalMemoryFlag = flag.Uint64("total-memory", 256*megabyte, "Total memory to allocate in bytes")
	timeoutSFlag    = flag.Int("timeout-s", 10, "Timeout in seconds. Set 0 for no timeout")
)

// A workload that allocates and the frees a bunch of pages on every CPU.
type workload struct {
	kmod        *kmod
	stats       *stats
	pagesPerCPU uint64
}

// per-CPU element of a workload. Assumes that the calling goroutine is already
// pinned to an appropriate CPU.
func (w *workload) runCPU(ctx context.Context) error {
	var pages []page

	defer func() {
		for _, page := range pages {
			w.kmod.freePage(page)
			w.stats.pagesFreed.Add(1)
		}
	}()

	for i := uint64(0); i < w.pagesPerCPU; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		page, err := w.kmod.allocPage()
		if err != nil {
			return fmt.Errorf("allocating page %d: %v", i, err)
		}
		w.stats.pagesAllocated.Add(1)

		pages = append(pages, page)
	}

	return nil
}

func doMain() error {
	file, err := os.Open("/proc/page_alloc_bench")
	if err != nil {
		return fmt.Errorf("Opening /proc/page_alloc_bench: %v", err)
	}
	kmod := kmod{file}
	defer kmod.Close()

	var stats stats

	workload := workload{
		kmod:        &kmod,
		stats:       &stats,
		pagesPerCPU: (*totalMemoryFlag / uint64(os.Getpagesize())) / uint64(runtime.NumCPU()),
	}
	fmt.Printf("Started %d threads, each allocating %d pages\n", runtime.NumCPU(), workload.pagesPerCPU)

	ctx := context.Background()
	if *timeoutSFlag != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*timeoutSFlag)*time.Second)
		defer cancel()
	}
	// The WaitGroup + errCh is a poor-man's sync.ErrGroup, that isn't in
	// the proper stdlib yet, and it doesn't seem worth throwing away the
	// no-Go-dependencies thing for that.
	var wg sync.WaitGroup
	errCh := make(chan error)
	for cpu := 0; cpu < runtime.NumCPU(); cpu++ {
		wg.Add(1)
		go func() {
			// This means that the goroutine gets the thread to
			// itself and the thread never gets migrated between
			// goroutines. IOW the goroutine "is a thread".
			runtime.LockOSThread()

			cpuMask := linux.NewCPUMask(cpu)
			err := linux.SchedSetaffinity(linux.PIDCallingThread, cpuMask)
			if err != nil {
				errCh <- fmt.Errorf("schedSetaffinity(%+v): %c", cpuMask, err)
			}

			err = workload.runCPU(ctx)
			if err != nil {
				errCh <- fmt.Errorf("workload failed on CPU %d: %v", cpu, err)
			}
			wg.Done()
		}()
	}

	// Poor-man's sync.ErrGroup.Wait()
	done := make(chan any)
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		break
	case err := <-errCh:
		return err
	}

	fmt.Printf("stats: %s", workload.stats.String())

	return nil
}

func main() {
	flag.Parse()

	if err := doMain(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
