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
	switch *workloadFlag {
	case "kallocfree":
		testDataPath, err := setupTestData(ctx, *testDataPathFlag, &c)
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
	case "findlimit":
		result, err := findlimit.Run(ctx, &findlimit.Options{})
		if err != nil {
			return err
		}
		fmt.Printf("Allocated %s\n", result.Allocated)
	case "composite":
		// Figure out how much memory the system appears to have when idle.
		fmt.Printf("Assessing system memory availability...\n")
		findlimitResult, err := findlimit.Run(ctx, &findlimit.Options{})
		if err != nil {
			return err
		}
		fmt.Printf("...Found %s available to userspace\n", findlimitResult.Allocated)

		// Make the system busy with lots of background kernel allocations and frees.
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		eg, ctx := errgroup.WithContext(ctx)
		kallocFree, err := kallocfree.New(ctx, &kallocfree.Options{
			TotalMemory: findlimitResult.Allocated - 128*pab.Megabyte,
		})
		if err != nil {
			return fmt.Errorf("setting up kallocfree workload: %v\n", err)
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

		// See how much memory seems to be in the system now.
		result, err := findlimit.Run(ctx, &findlimit.Options{})
		if err != nil {
			return err
		}
		fmt.Printf("Result: %s (down from %s)\n", result.Allocated, findlimitResult.Allocated)

	default:
		return fmt.Errorf("Invalid value for --workload - %q. Available: %s\n", *workloadFlag, allWorkloads)
	case "?":
		fmt.Fprintf(os.Stdout, "Available workloads: %s\n", allWorkloads)
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
