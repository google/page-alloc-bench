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
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync/atomic"
	"syscall"

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

type workload struct {
	kmod         *kmod.Connection
	stats        *stats
	testDataPath string // Path to a file with some data in it.
	pagesPerCPU  int64
	*pab.Cleanups
}

// io.Reader that produces some sort of bytes that aren't all the same value.
// Create a bunch of data we can later use to fill up the page cache.
// Returns file path.
func setupTestData(ctx context.Context, path string, c *pab.Cleanups) (string, error) {
	// Hacks for fast development.
	var f *os.File
	if path != "" {
		_, err := os.Stat(path)
		if err == nil { // Already exists?
			fmt.Printf("Reusing data file %v\n", path)
			return path, nil
		}
		fmt.Printf("Creating reusable data file (stat returned: %v) %v\n", err, path)
		f, err = os.Create(path)
		if err != nil {
			return "", fmt.Errorf("making source data file: %v", err)
		}
	} else {
		var err error
		f, err = os.CreateTemp("", "")
		if err != nil {
			return "", fmt.Errorf("making source data file: %v", err)
		}
		c.Cleanup(func() { os.Remove(f.Name()) })
	}

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
	_, err := io.Copy(f, &io.LimitedReader{rand.Reader, (1 * pab.Gigabyte).Bytes()})
	if err != nil {
		return "", fmt.Errorf("writing data to populate page cache: %v", err)
	}

	// Sync so the pages aren't dirty.
	syscall.Sync()

	return f.Name(), nil
}

// Run once on the system before each iteration of the workload.
func (w *workload) setup(ctx context.Context) error {
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

	for i := int64(0); i < w.pagesPerCPU; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		page, err := w.kmod.AllocPage(0)
		if err != nil {
			return fmt.Errorf("allocating page %d: %v", i, err)
		}
		w.stats.pagesAllocated.Add(1)

		pages = append(pages, page)
	}

	return nil
}

// Overall run function, call once from anywhere.
func (w *workload) run(ctx context.Context) error {
	fmt.Printf("Running global workload setup\n")
	w.setup(ctx)

	fmt.Printf("Started %d threads, each allocating %d pages\n", runtime.NumCPU(), w.pagesPerCPU)

	// The WaitGroup + errCh is a poor-man's sync.ErrGroup, that isn't in
	// the proper stdlib yet, and it doesn't seem worth throwing away the
	// no-Go-dependencies thing for that.
	eg, ctx := errgroup.WithContext(ctx)
	for cpu := 0; cpu < runtime.NumCPU(); cpu++ {
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

// Run runs the workload
func Run(ctx context.Context, opts *Options) error {
	file, err := os.Open("/proc/page_alloc_bench")
	if err != nil {
		return fmt.Errorf("Opening /proc/page_alloc_bench: %v", err)
	}
	kmod := kmod.Connection{file}
	defer kmod.Close()

	var c pab.Cleanups
	defer c.Run()
	testDataPath, err := setupTestData(ctx, opts.TestDataPath, &c)
	if err != nil {
		return err
	}

	var stats stats

	workload := workload{
		kmod:         &kmod,
		stats:        &stats,
		testDataPath: testDataPath,
		pagesPerCPU:  opts.TotalMemory.Pages() / int64(runtime.NumCPU()),
		Cleanups:     &c,
	}

	err = workload.run(ctx)
	fmt.Printf("stats: %s\n", workload.stats.String())

	return err
}
