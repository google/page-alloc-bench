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
	"io"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/page_alloc_bench/kmod"
	"github.com/google/page_alloc_bench/linux"
)

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
	return fmt.Sprintf("pagesAllocated=%d pagesFreed=%d ", s.pagesAllocated.Load(), s.pagesFreed.Load())
}

var (
	totalMemoryFlag = flag.Uint64("total-memory", 256*megabyte, "Total memory to allocate in bytes")
	timeoutSFlag    = flag.Int("timeout-s", 10, "Timeout in seconds. Set 0 for no timeout")
)

// A workload that allocates and the frees a bunch of pages on every CPU.
type workload struct {
	kmod        *kmod.Connection
	stats       *stats
	pagesPerCPU uint64
	cleanups    []func()
}

// cleanup is like testing.T.Cleanup.
func (w *workload) cleanup(f func()) {
	w.cleanups = append(w.cleanups, f)
}

func (w *workload) runCleanups() {
	for _, f := range w.cleanups {
		f()
	}
}

// io.Reader that produces some sort of bytes that aren't all the same value.
// Can't use rand due to https://github.com/golang/go/issues/64943
type variedReader struct {
	i byte
}

func (r *variedReader) Read(p []byte) (n int, err error) {
	for o := 0; o < len(p); o++ {
		p[o] = r.i
		if r.i == 255 {
			r.i = 0
		} else {
			r.i++
		}
	}
	return len(p), nil
}

// Initial setup part of a workload - run only once on the system.
func (w *workload) setup(ctx context.Context) error {
	// Create a bunch of page cache data.

	f, err := os.CreateTemp("", "")
	if err != nil {
		return fmt.Errorf("making tempfile: %v", err)
	}
	w.cleanup(func() { os.Remove(f.Name()) })

	// Close the file when the context is canceled, to abort the io.Copy. This
	// probably won't shut down cleanly, although this logic could be extended
	// to do so if necessary. Note also the f.Close races with the os.Remove,
	// this is harmless.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-ctx.Done()
		f.Close()
	}()

	// Write a bunch of bytes that aren't just all zero or whatever.
	_, err = io.Copy(f, &io.LimitedReader{&variedReader{}, 1 * gigabyte})
	if err != nil {
		return fmt.Errorf("writing data to populate page cache: %v", err)
	}
	return nil
}

// per-CPU element of a workload. Assumes that the calling goroutine is already
// pinned to an appropriate CPU.
func (w *workload) runCPU(ctx context.Context) error {
	var pages []kmod.Page

	defer func() {
		for _, page := range pages {
			w.kmod.FreePage(page)
			w.stats.pagesFreed.Add(1)
		}
	}()

	for i := uint64(0); i < w.pagesPerCPU; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		page, err := w.kmod.AllocPage()
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
	kmod := kmod.Connection{file}
	defer kmod.Close()

	var stats stats

	workload := workload{
		kmod:        &kmod,
		stats:       &stats,
		pagesPerCPU: (*totalMemoryFlag / uint64(os.Getpagesize())) / uint64(runtime.NumCPU()),
	}
	defer workload.runCleanups()

	ctx := context.Background()
	if *timeoutSFlag != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*timeoutSFlag)*time.Second)
		defer cancel()
	}

	fmt.Printf("Running global workload setup\n")
	workload.setup(ctx)

	fmt.Printf("Started %d threads, each allocating %d pages\n", runtime.NumCPU(), workload.pagesPerCPU)

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
