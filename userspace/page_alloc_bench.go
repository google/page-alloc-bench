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
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/page_alloc_bench/pab"
	"github.com/google/page_alloc_bench/workload/findlimit"
	"github.com/google/page_alloc_bench/workload/kallocfree"
	"golang.org/x/sync/errgroup"
)

var (
	timeoutSFlag   = flag.Int("timeout-s", 0, "Timeout in seconds. Set 0 for no timeout (default)")
	outputPathFlag = flag.String("output-path", "", "File to write JSON results to. See README for specification.")
)

type Result struct {
	MemoryAvailableBytes int64 `json:"memory_available_bytes"`
}

func run(ctx context.Context) (*Result, error) {
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
	var result Result
	eg.Go(func() error {
		// See how much memory seems to be in the system now.
		findlimitResult2, err := findlimit.Run(ctx, &findlimit.Options{})
		if err != nil {
			return fmt.Errorf("antagonized findlimit run: %v\n", err)
		}
		fmt.Printf("Result: %s (down from %s)\n", findlimitResult2.Allocated, findlimitResult1.Allocated)
		result.MemoryAvailableBytes = findlimitResult2.Allocated.Bytes()
		cancel() // Done.
		return nil
	})
	return &result, eg.Wait()
}

func writeOutput(path string, result *Result) error {
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

	result, err := run(ctx)
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
