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
	"math/rand"
	"os"
	"runtime"
	"slices"
	"sync/atomic"
	"time"

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
	pagesAllocated        atomic.Uint64
	pagesFreed            atomic.Uint64
	numaRemoteAllocations atomic.Uint64
	latencies             [][]time.Duration // Per CPU worker.
}

type Result struct {
	PagesAllocated        uint64 // Only incremented; subtract pagesFreed to count leaks.
	PagesFreed            uint64
	NUMARemoteAllocations uint64          // Number of pages where page NID didn't match CPU's NID.
	Latencies             []time.Duration // Excludes userspace/syscall overhead. We only capture the last N allocations.
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
	cpuToNode          map[int]int
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
func (w *Workload) runCPU(ctx context.Context, cpu int) error {
	var pages []*kmod.Page

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

	// Give each CPU its own pattern of behaviour, but keep the pattern
	// stable between runs (at least for the same build)..
	random := rand.New(rand.NewSource(int64(cpu)))
	steady := false

	for ctx.Err() == nil {
		// Pattern is to allocate and free in alternate bursts while
		// keeping the overall number of allocated pages bouncing around
		// a roughly stable "middle" value.
		middle := 50000
		var target int
		if random.Uint32()%2 == 0 {
			target = middle + (int(random.Uint64() % 1000))
		} else {
			target = middle - (int(random.Uint64() % 1000))
		}

		// Allocate up to target.
		for len(pages) < target {
			page, err := w.allocPageOnCPU(0, cpu)
			if err != nil {
				return err
			}
			pages = append(pages, page)

			// We are steady once we hit the middle at least once.
			// Note it might take a few iterations before we hit
			// this point, that's fine.
			if len(pages) == middle && !steady {
				if w.steadyStateThreads.Add(1) >= int32(w.numThreads) {
					close(w.steadyStateReached)
				}
				steady = true
			}
		}

		// Free down to target.
		for len(pages) > target {
			if err := w.kmod.FreePage(pages[0]); err != nil {
				return fmt.Errorf("freeing page: %v\n", err)
			}
			pages = pages[1:]
		}
	}

	return nil
}

// Allocate a page, update stats. Caller must be running on the stated CPU.
func (w *Workload) allocPageOnCPU(order int, cpu int) (*kmod.Page, error) {
	page, err := w.kmod.AllocPage(order)
	if err != nil {
		return nil, fmt.Errorf("allocating page: %v", err)
	}
	w.stats.pagesAllocated.Add(1)
	if page.NID != w.cpuToNode[cpu] {
		w.stats.numaRemoteAllocations.Add(1)
	}
	l := &w.stats.latencies[cpu]
	*l = append(*l, page.Latency)
	if len(*l) > 50000 {
		*l = (*l)[1:]
	}
	return page, nil
}

// Run runs the workload. This workload runs continuously until cancellation,
// then returns nil. You may only call this merthod once.
func (w *Workload) Run(ctx context.Context) (*Result, error) {
	defer w.kmod.Close()

	fmt.Printf("Running global workload setup\n")
	w.setup(ctx)

	fmt.Printf("Started %d threads, each allocating %d pages\n", runtime.NumCPU(), w.pagesPerCPU)

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

			err = w.runCPU(ctx, cpu)
			if err != nil {
				return fmt.Errorf("workload failed on CPU %d: %v", cpu, err)
			}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}
	r := Result{
		PagesAllocated:        w.stats.pagesAllocated.Load(),
		PagesFreed:            w.stats.pagesFreed.Load(),
		NUMARemoteAllocations: w.stats.numaRemoteAllocations.Load(),
		Latencies:             slices.Concat(w.stats.latencies...),
	}
	return &r, nil
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

	nodes, err := linux.NUMANodes()
	if err != nil {
		return nil, fmt.Errorf("parsing NUMA nodes: %v", err)
	}
	cpuToNode := make(map[int]int)
	for nid, mask := range nodes {
		for _, cpu := range mask {
			cpuToNode[int(cpu)] = nid
		}
	}
	for cpu := 0; cpu < runtime.NumCPU(); cpu++ {
		if _, ok := cpuToNode[cpu]; !ok {
			return nil, fmt.Errorf("found no NUMA node for CPU %d (nodes: %+v)", cpu, nodes)
		}
	}

	return &Workload{
		kmod: &kmod,
		stats: &stats{
			latencies: make([][]time.Duration, runtime.NumCPU()),
		},
		pagesPerCPU:        opts.TotalMemory.Pages() / int64(runtime.NumCPU()),
		testDataPath:       opts.TestDataPath,
		steadyStateReached: make(chan struct{}),
		numThreads:         runtime.NumCPU(),
		cpuToNode:          cpuToNode,
	}, nil
}
