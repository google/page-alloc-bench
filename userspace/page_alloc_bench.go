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
	"strconv"
	"strings"
	"time"

	"github.com/google/page_alloc_bench/pab"
	"github.com/google/page_alloc_bench/workload/findlimit"
	"github.com/google/page_alloc_bench/workload/kallocfree"
	"golang.org/x/sync/errgroup"
)

var (
	timeoutSFlag    = flag.Int("timeout-s", 0, "Timeout in seconds. Set 0 for no timeout (default)")
	outputPathFlag  = flag.String("output-path", "", "File to write JSON results to. See README for specification.")
	iterationsFlag  = flag.Int("iterations", 5, "Iterations")
	allocOrdersFlag = flag.String("alloc-orders", "0,4", "Comma-separate list of page alloc orders to test")
)

var (
	kernelAllocFailuresPrefix        = "kernel_alloc_failures"
	idleAvailableBytesPrefix         = "idle_available_bytes"
	antagonizedAvailableBytesPrefix  = "antagonized_available_bytes"
	kernelPageAllocsPrefix           = "kernel_page_allocs"
	kernelPageAllocsRemotePrefix     = "kernel_page_allocs_remote"
	kernelPageAllocLatenciesNSPrefix = "kernel_page_alloc_latencies_ns"
)

// Runs findlimit workload @iterations times, returns available byte counts.
func repeatFindlimit(ctx context.Context, iterations int, desc string) ([]int64, error) {
	var result []int64
	for i := 1; i <= iterations; i++ {
		if ctx.Err() != nil {
			return nil, nil
		}
		findlimitResult, err := findlimit.Run(ctx, &findlimit.Options{})
		if err != nil {
			return nil, fmt.Errorf("%s findlimit run %d: %v\n", desc, i, err)
		}
		fmt.Printf("\tIteration %d/%d: %s available on %s system\n",
			i, *iterationsFlag, findlimitResult.Allocated, desc)
		result = append(result, findlimitResult.Allocated.Bytes())
	}
	return result, nil
}

// Returns map of metric names to values. Metrics with a single value are just a
// slice with only one item.
func run(ctx context.Context, allocOrder int) (map[string][]int64, error) {
	result := make(map[string][]int64)

	// We're not running this just yet, btu set it upt now to fail fast.
	kernelUsage := 128 * pab.Megabyte
	kallocFree, err := kallocfree.New(ctx, &kallocfree.Options{
		TotalMemory: kernelUsage,
		Order:       allocOrder,
	})
	if err != nil {
		return nil, fmt.Errorf("setting up kallocfree workload: %v\n", err)
	}

	// Figure out how much memory the system appears to have when idle.
	fmt.Printf("Assessing system memory availability...\n")
	idleAvailableBytes, err := repeatFindlimit(ctx, *iterationsFlag, "initial")
	if err != nil {
		return nil, err
	}
	result[idleAvailableBytesPrefix] = idleAvailableBytes

	// Make the system busy with lots of background kernel allocations and frees.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		kallocfreeResult, err := kallocFree.Run(ctx)
		if err != nil {
			return err
		}
		result[kernelAllocFailuresPrefix] = []int64{int64(kallocfreeResult.AllocFailures)}
		result[kernelPageAllocsPrefix] = []int64{int64(kallocfreeResult.PagesAllocated)}
		result[kernelPageAllocsRemotePrefix] = []int64{int64(kallocfreeResult.NUMARemoteAllocations)}
		var ls []int64
		for _, l := range kallocfreeResult.Latencies {
			ls = append(ls, l.Nanoseconds())
		}
		result[kernelPageAllocLatenciesNSPrefix] = ls
		return nil
	})
	fmt.Printf("Waiting for kallocfree to reach steady state...\n")
	kallocFree.AwaitSteadyState(ctx)
	fmt.Printf("...Steady state reached.\n")
	eg.Go(func() error {
		// See how much memory seems to be in the system now.
		antagonizedAvailableBytes, err := repeatFindlimit(ctx, *iterationsFlag, "antagonized")
		if err != nil {
			return err
		}
		result[antagonizedAvailableBytesPrefix] = antagonizedAvailableBytes
		cancel() // Done.
		return nil
	})
	return result, eg.Wait()
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

func printResult(result map[string][]int64) {
	// Print in order sorted by last character, so that all _orderN
	// metrics for same N get printed together.
	keys := []string{}
	for key, _ := range result {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(k1, k2 string) int {
		// Careful - indexing a string gives you a byte which is
		// unsigned. Need to convert to int _before_ subtracting.
		return int(k1[len(k1)-1]) - int(k2[len(k2)-1])
	})

	for _, key := range keys {
		val := result[key]
		if len(val) > 1 {
			printAverages(key, val)
		} else {
			fmt.Printf("%q: %v\n", key, val[0])
		}
	}
}

func writeOutput(path string, result map[string][]int64) error {
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

	orderStrs := strings.Split(*allocOrdersFlag, ",")
	if len(orderStrs) == 0 {
		return fmt.Errorf("--alloc-orders empty?")
	}
	var orders []int
	for _, orderStr := range orderStrs {
		o, err := strconv.Atoi(orderStr)
		if err != nil {
			return fmt.Errorf("Bad value %q in --alloc-orders: %v", orderStr, err)
		}
		orders = append(orders, o)
	}

	result := make(map[string][]int64)
	for _, order := range orders {
		orderResult, err := run(ctx, order)
		if err != nil {
			return err
		}

		for key, val := range orderResult {
			result[fmt.Sprintf("%s_order%d", key, order)] = val
		}
	}

	printResult(result)

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
