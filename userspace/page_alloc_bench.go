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
	"math"
	"os"
	"slices"
	"time"

	"github.com/google/page_alloc_bench/pab"
	"github.com/google/page_alloc_bench/workload/findlimit"
	"github.com/google/page_alloc_bench/workload/kallocfree"
	"golang.org/x/sync/errgroup"
)

var (
	timeoutSFlag   = flag.Int("timeout-s", 0, "Timeout in seconds. Set 0 for no timeout (default)")
	outputPathFlag = flag.String("output-path", "", "File to write JSON results to. See README for specification.")
	iterationsFlag = flag.Int("iterations", 5, "Iterations")
)

type Result struct {
	IdleAvailableBytes         []int64 `json:"idle_available_bytes"`
	AntagonizedAvailableBytes  []int64 `json:"antagonized_available_bytes"`
	KernelPageAllocs           int64   `json:"kernel_page_allocs"`
	KernelPageAllocsRemote     int64   `json:"kernel_page_allocs_remote"`
	KernelPageAllocLatenciesNS []int64 `json:"kernel_page_alloc_latencies_ns"`
}

// Runs findlimit workload @iterations times, appends result to @result.
func repeatFindlimit(ctx context.Context, iterations int, result *[]int64, desc string) error {
	for i := 1; i <= iterations; i++ {
		if ctx.Err() != nil {
			return nil
		}
		findlimitResult, err := findlimit.Run(ctx, &findlimit.Options{})
		if err != nil {
			return fmt.Errorf("%s findlimit run %d: %v\n", desc, i, err)
		}
		fmt.Printf("\tIteration %d/%d: %s available on %s system\n",
			i, *iterationsFlag, findlimitResult.Allocated, desc)
		*result = append(*result, findlimitResult.Allocated.Bytes())
	}
	return nil
}

func run(ctx context.Context) (*Result, error) {
	var result Result

	// We're not running this just yet, btu set it upt now to fail fast.
	kernelUsage := 128 * pab.Megabyte
	kallocFree, err := kallocfree.New(ctx, &kallocfree.Options{
		TotalMemory: kernelUsage,
	})
	if err != nil {
		return nil, fmt.Errorf("setting up kallocfree workload: %v\n", err)
	}

	// Figure out how much memory the system appears to have when idle.
	fmt.Printf("Assessing system memory availability...\n")
	if err := repeatFindlimit(ctx, *iterationsFlag, &result.IdleAvailableBytes, "initial"); err != nil {
		return nil, err
	}

	// Make the system busy with lots of background kernel allocations and frees.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		kallocfreeResult, err := kallocFree.Run(ctx)
		if err != nil {
			return err
		}
		result.KernelPageAllocs += int64(kallocfreeResult.PagesAllocated)
		result.KernelPageAllocsRemote += int64(kallocfreeResult.NUMARemoteAllocations)
		for _, l := range kallocfreeResult.Latencies {
			result.KernelPageAllocLatenciesNS = append(result.KernelPageAllocLatenciesNS, l.Nanoseconds())
		}
		return nil
	})
	fmt.Printf("Waiting for kallocfree to reach steady state...\n")
	kallocFree.AwaitSteadyState(ctx)
	fmt.Printf("...Steady state reached.\n")
	eg.Go(func() error {
		// See how much memory seems to be in the system now.
		if err := repeatFindlimit(ctx, *iterationsFlag, &result.AntagonizedAvailableBytes, "antagonized"); err != nil {
			return err
		}
		cancel() // Done.
		return nil
	})
	return &result, eg.Wait()
}

func printAverages(name string, vals []int64) {
	if len(vals) == 0 {
		fmt.Printf("No values for metric %q\n", name)
		return
	}
	sum := int64(0)
	max := int64(math.MinInt64)
	min := int64(math.MaxInt64)
	// Hack: we happen to know the biggest numbers we're using here
	// are nanosecond latencies. It ought to be impossible to
	// overflow here so we dont bother doing fancy maths.
	for _, val := range vals {
		if val > max {
			max = val
		}
		if val < min {
			min = val
		}
		sum += val
	}

	sorted := slices.Clone(vals)
	slices.Sort(sorted)
	mean := float64(sum) / float64(len(vals))
	median := sorted[len(sorted)/2]
	p95 := sorted[(len(sorted)*95)/100]
	fmt.Printf("%q:\n\tsamples: %d\n\tmean: %12.02f\n\tmed: %12d\n\tp95: %12d\n\tmax: %12d\n\tmin: %12d\n",
		name, len(vals), mean, median, p95, max, min)
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

	printAverages("idle_available_bytes", result.IdleAvailableBytes)
	printAverages("antagonized_available_bytes", result.AntagonizedAvailableBytes)
	printAverages("kernel_page_alloc_latencies_ns", result.KernelPageAllocLatenciesNS)

	if *outputPathFlag != "" {
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
