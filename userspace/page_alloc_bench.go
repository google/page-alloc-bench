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
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/google/page_alloc_bench/pab"
	"github.com/google/page_alloc_bench/workload/findlimit"
	"github.com/google/page_alloc_bench/workload/kallocfree"
	"golang.org/x/sync/errgroup"
)

var (
	workloadFlag     = flag.String("workload", "", "Worklad to run. Required. Pass ? to see available workloads")
	totalMemoryFlag  = flag.Int64("total-memory", (256 * pab.Megabyte).Bytes(), "Total memory to allocate in bytes")
	timeoutSFlag     = flag.Int("timeout-s", 10, "Timeout in seconds. Set 0 for no timeout")
	testDataPathFlag = flag.String("test-data-path", "", "For dev, path to reuse for test data")
	outputPathFlag   = flag.String("output-path", "", "File to write JSON results to. See README for specification.")
)

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

func runKallocfree(ctx context.Context, c *pab.Cleanups) error {
	testDataPath, err := setupTestData(ctx, *testDataPathFlag, c)
	if err != nil {
		return err
	}

	wl, err := kallocfree.New(ctx, &kallocfree.Options{
		TotalMemory:  pab.ByteSize(*totalMemoryFlag),
		TestDataPath: testDataPath,
	})
	if err != nil {
		return fmt.Errorf("setting up kallocfree workload: %v\n", err)
	}
	return wl.Run(ctx)
}

func runFindlimit(ctx context.Context) error {
	result, err := findlimit.Run(ctx, &findlimit.Options{})
	if err != nil {
		return err
	}
	fmt.Printf("Allocated %s\n", result.Allocated)
	return nil
}

// Result for the "composite" benchmark. Fields specified in README.
type CompositeResult struct {
	MemoryAvailableDiffBytes int64 `json:"memory_available_diff_bytes"`
}

func runComposite(ctx context.Context) (*CompositeResult, error) {
	// Figure out how much memory the system appears to have when idle.
	fmt.Printf("Assessing system memory availability...\n")
	findlimitResult1, err := findlimit.Run(ctx, &findlimit.Options{})
	if err != nil {
		return nil, fmt.Errorf("initial findlimit run: %v\n", err)
	}
	fmt.Printf("...Found %s available to userspace\n", findlimitResult1.Allocated)

	// Make the system busy with lots of background kernel allocations and frees.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	eg, ctx := errgroup.WithContext(ctx)
	kernelUsage := 128 * pab.Megabyte
	kallocFree, err := kallocfree.New(ctx, &kallocfree.Options{
		TotalMemory: kernelUsage,
	})
	if err != nil {
		return nil, fmt.Errorf("setting up kallocfree workload: %v\n", err)
	}
	eg.Go(func() error {
		for {
			err := kallocFree.Run(ctx)
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return err
			}
		}
	})
	fmt.Printf("Waiting for kallocfree to reach steady state...\n")
	kallocFree.AwaitSteadyState(ctx)
	fmt.Printf("...Steady state reached.\n")
	var result CompositeResult
	eg.Go(func() error {
		// See how much memory seems to be in the system now.
		findlimitResult2, err := findlimit.Run(ctx, &findlimit.Options{})
		if err != nil {
			return fmt.Errorf("antagonized findlimit run: %v\n", err)
		}
		fmt.Printf("Result: %s (down from %s)\n", findlimitResult2.Allocated, findlimitResult1.Allocated)
		diff := findlimitResult1.Allocated - (findlimitResult2.Allocated + kernelUsage)
		fmt.Printf("Diff in memory availability: %s\n", diff)
		result.MemoryAvailableDiffBytes = diff.Bytes()
		cancel() // Done.
		return nil
	})
	return &result, eg.Wait()
}

func writeOutput(path string, result *CompositeResult) error {
	output, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshalling JSON output: %v\n", err)
	}
	return os.WriteFile(path, output, 0644)
}

func doMain() error {
	ctx := context.Background()
	if *timeoutSFlag != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*timeoutSFlag)*time.Second)
		defer cancel()
	}

	var c pab.Cleanups
	defer c.Run()
	allWorkloads := "kallocfree, findlimit, composite"
	var result *CompositeResult
	var err error
	switch *workloadFlag {
	case "kallocfree":
		err = runKallocfree(ctx, &c)
	case "findlimit":
		err = runFindlimit(ctx)
	case "composite":
		result, err = runComposite(ctx)
	default:
		return fmt.Errorf("Invalid value for --workload - %q. Available: %s\n", *workloadFlag, allWorkloads)
	case "?":
		fmt.Fprintf(os.Stdout, "Available workloads: %s\n", allWorkloads)
	}
	if err != nil {
		return err
	}

	if *outputPathFlag != "" {
		if result == nil {
			return fmt.Errorf("this workload doesn't support --outputpath")
		}
		return writeOutput(*outputPathFlag, result)
	}
	return nil
}

func main() {
	flag.Parse()

	if err := doMain(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
