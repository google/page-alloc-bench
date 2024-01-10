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

// Package kallocfree contains a workload that allocates and frees kernel memory
// on all CPUs.
package kallocfree

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync/atomic"

	"github.com/google/page_alloc_bench/kmod"
	"github.com/google/page_alloc_bench/linux"
	"github.com/google/page_alloc_bench/pab"
	"golang.org/x/sync/errgroup"
)

type Options struct {
	// See corresponding cmdline flags for explanation of fields.
	TotalMemory  pab.ByteSize
	TestDataPath string
}

type stats struct {
	pagesAllocated atomic.Uint64 // Only incremented; subtract pagesFreed to count leaks.
	pagesFreed     atomic.Uint64
}

func (s *stats) String() string {
	return fmt.Sprintf("pagesAllocated=%d pagesFreed=%d ", s.pagesAllocated.Load(), s.pagesFreed.Load())
}

type Workload struct {
	kmod               *kmod.Connection
	stats              *stats
	testDataPath       string // Path to a file with some data in it. Optional.
	pagesPerCPU        int64
	numThreads         int
	steadyStateThreads atomic.Int32
	steadyStateReached chan struct{} // Will be closed when stateStateThreads reaches numThreads
	*pab.Cleanups
}

// Run once on the system before each iteration of the workload.
func (w *Workload) setup(ctx context.Context) error {
	if w.testDataPath == "" {
		return nil
	}
	// Read some data to populate the page cache a bit.
	f, err := os.Open(w.testDataPath)
	if err != nil {
		return fmt.Errorf("opening data to fill page cache: %v")
	}
	fmt.Printf("Reading %v\n", w.testDataPath)
	_, err = io.Copy(io.Discard, f)
	fmt.Printf("Done reading %v\n", w.testDataPath)
	return err
}

var freeErrorLogged = false

// per-CPU element of a workload. Assumes that the calling goroutine is already
// pinned to an appropriate CPU.
func (w *Workload) runCPU(ctx context.Context) error {
	var pages []kmod.Page

	defer func() {
		for _, page := range pages {
			if err := w.kmod.FreePage(page); err != nil && !freeErrorLogged {
				// The kmod also frees on rmmod so it might be OK.
				fmt.Fprintf(os.Stderr, "Couldn't free one or more kernel pages, consider rebooting: %v\n", err)
				freeErrorLogged = true
			}
			w.stats.pagesFreed.Add(1)
		}
	}()

	steady := false

	for ctx.Err() == nil {
		page, err := w.kmod.AllocPage(0)
		if err != nil {
			return fmt.Errorf("allocating page: %v", err)
		}
		w.stats.pagesAllocated.Add(1)

		pages = append(pages, page)

		if int64(len(pages)) >= w.pagesPerCPU {
			if !steady {
				if w.steadyStateThreads.Add(1) >= int32(w.numThreads) {
					close(w.steadyStateReached)
				}
				steady = true
			}

			if err := w.kmod.FreePage(pages[0]); err != nil {
				return fmt.Errorf("freeing page: %v\n", err)
			}
			pages = pages[1:]
		}
	}

	return nil
}

// Run runs the workload. This workload runs continuously until cancellation,
// then returns nil. You may only call this merthod once.
func (w *Workload) Run(ctx context.Context) error {
	defer w.kmod.Close()

	fmt.Printf("Running global workload setup\n")
	w.setup(ctx)

	fmt.Printf("Started %d threads, each allocating %d pages\n", runtime.NumCPU(), w.pagesPerCPU)

	// The WaitGroup + errCh is a poor-man's sync.ErrGroup, that isn't in
	// the proper stdlib yet, and it doesn't seem worth throwing away the
	// no-Go-dependencies thing for that.
	eg, ctx := errgroup.WithContext(ctx)
	for cpu := 0; cpu < w.numThreads; cpu++ {
		eg.Go(func() error {
			// This means that the goroutine gets the thread to
			// itself and the thread never gets migrated between
			// goroutines. IOW the goroutine "is a thread".
			runtime.LockOSThread()

			cpuMask := linux.NewCPUMask(cpu)
			err := linux.SchedSetaffinity(linux.PIDCallingThread, cpuMask)
			if err != nil {
				return fmt.Errorf("SchedSetaffinity(%+v): %c", cpuMask, err)
			}

			err = w.runCPU(ctx)
			if err != nil {
				return fmt.Errorf("workload failed on CPU %d: %v", cpu, err)
			}
			return nil
		})
	}

	return eg.Wait()
}

// AwaitSteadyState blocks until the workload can be expected to be allocating
// and freeing pages at the same rate.
func (w *Workload) AwaitSteadyState(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-w.steadyStateReached:
	}
}

func New(ctx context.Context, opts *Options) (*Workload, error) {
	file, err := os.Open("/proc/page_alloc_bench")
	if err != nil {
		return nil, fmt.Errorf("opening /proc/page_alloc_bench: %v", err)
	}
	kmod := kmod.Connection{file}

	return &Workload{
		kmod:               &kmod,
		stats:              &stats{},
		pagesPerCPU:        opts.TotalMemory.Pages() / int64(runtime.NumCPU()),
		testDataPath:       opts.TestDataPath,
		steadyStateReached: make(chan struct{}),
		numThreads:         runtime.NumCPU(),
	}, nil
}
