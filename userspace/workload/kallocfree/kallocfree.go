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
	kmod           *kmod.Connection
	stats          *stats
	variedDataPath string // Path to a file with some data in it.
	pagesPerCPU    int64
	*pab.Cleanups
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
func setupVariedData(ctx context.Context, path string, c *pab.Cleanups) (string, error) {
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
	_, err := io.Copy(f, &io.LimitedReader{&variedReader{}, (1 * pab.Gigabyte).Bytes()})
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
	variedDataPath, err := setupVariedData(ctx, opts.TestDataPath, &c)
	if err != nil {
		return err
	}

	var stats stats

	workload := workload{
		kmod:           &kmod,
		stats:          &stats,
		variedDataPath: variedDataPath,
		pagesPerCPU:    opts.TotalMemory.Pages() / int64(runtime.NumCPU()),
		Cleanups:       &c,
	}

	err = workload.run(ctx)
	fmt.Printf("stats: %s\n", workload.stats.String())

	return err
}
