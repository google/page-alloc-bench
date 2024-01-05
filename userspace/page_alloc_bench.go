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
	"slices"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/page_alloc_bench/kmod"
	"github.com/google/page_alloc_bench/linux"
	"golang.org/x/sync/errgroup"
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
	totalMemoryFlag  = flag.Uint64("total-memory", 256*megabyte, "Total memory to allocate in bytes")
	timeoutSFlag     = flag.Int("timeout-s", 10, "Timeout in seconds. Set 0 for no timeout")
	testDataPathFlag = flag.String("test-data-path", "", "For dev, path to reuse for test data")
)

// cleanups provides functionality like testing.T.Cleanup.
type cleanups struct {
	funcs []func()
}

// cleanup adds a cleanup.
func (c *cleanups) cleanup(f func()) {
	c.funcs = append(c.funcs, f)
}

// run runs the cleanups in the reverse order of the cleanup() calls.
func (c *cleanups) run() {
	slices.Reverse(c.funcs)
	for _, f := range c.funcs {
		f()
	}
	c.funcs = nil
}

// A workload that allocates and the frees a bunch of pages on every CPU.
type workload struct {
	kmod           *kmod.Connection
	stats          *stats
	variedDataPath string // Path to a file with some data in it.
	pagesPerCPU    uint64
	cleanups
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

// Create a bunch of data we can later use to fill up the page cache.
// Returns file path.
func setupVariedData(ctx context.Context, path string, c *cleanups) (string, error) {
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
		c.cleanup(func() { os.Remove(f.Name()) })
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
	_, err := io.Copy(f, &io.LimitedReader{&variedReader{}, 1 * gigabyte})
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
	f, err := os.Open(w.variedDataPath)
	if err != nil {
		return fmt.Errorf("opening data to fill page cache: %v")
	}
	fmt.Printf("Reading %v\n", w.variedDataPath)
	_, err = io.Copy(io.Discard, f)
	fmt.Printf("Done reading %v\n", w.variedDataPath)
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

	for i := uint64(0); i < w.pagesPerCPU; i++ {
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

func doMain() error {
	file, err := os.Open("/proc/page_alloc_bench")
	if err != nil {
		return fmt.Errorf("Opening /proc/page_alloc_bench: %v", err)
	}
	kmod := kmod.Connection{file}
	defer kmod.Close()

	ctx := context.Background()
	if *timeoutSFlag != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*timeoutSFlag)*time.Second)
		defer cancel()
	}

	var cleanups cleanups
	defer cleanups.run()
	variedDataPath, err := setupVariedData(ctx, *testDataPathFlag, &cleanups)
	if err != nil {
		return err
	}

	var stats stats

	workload := workload{
		kmod:           &kmod,
		stats:          &stats,
		variedDataPath: variedDataPath,
		pagesPerCPU:    (*totalMemoryFlag / uint64(os.Getpagesize())) / (2 * uint64(runtime.NumCPU())),
		cleanups:       cleanups,
	}

	fmt.Printf("Running global workload setup\n")
	workload.setup(ctx)

	fmt.Printf("Started %d threads, each allocating %d pages\n", runtime.NumCPU(), workload.pagesPerCPU)

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

			err = workload.runCPU(ctx)
			if err != nil {
				return fmt.Errorf("workload failed on CPU %d: %v", cpu, err)
			}
			return nil
		})
	}

	err = eg.Wait()

	fmt.Printf("stats: %s\n", workload.stats.String())

	return err
}

func main() {
	flag.Parse()

	if err := doMain(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
