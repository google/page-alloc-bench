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
	"os"
	"time"

	"github.com/google/page_alloc_bench/pab"
	"github.com/google/page_alloc_bench/workload/findlimit"
	"github.com/google/page_alloc_bench/workload/kallocfree"
)

var (
	workloadFlag     = flag.String("workload", "", "Worklad to run. Required. Pass ? to see available workloads")
	totalMemoryFlag  = flag.Int64("total-memory", (256 * pab.Megabyte).Bytes(), "Total memory to allocate in bytes")
	timeoutSFlag     = flag.Int("timeout-s", 10, "Timeout in seconds. Set 0 for no timeout")
	testDataPathFlag = flag.String("test-data-path", "", "For dev, path to reuse for test data")
)

func doMain() error {
	ctx := context.Background()
	if *timeoutSFlag != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*timeoutSFlag)*time.Second)
		defer cancel()
	}

	allWorkloads := "kallocfree, findlimit, composite"
	switch *workloadFlag {
	case "kallocfree":
		return kallocfree.Run(ctx, &kallocfree.Options{
			TotalMemory:  pab.ByteSize(*totalMemoryFlag),
			TestDataPath: *testDataPathFlag,
		})
	case "findlimit":
		return findlimit.Run(ctx, &findlimit.Options{
			AllocSize: 128 * pab.Megabyte,
		})
	default:
		return fmt.Errorf("Invalid value for --workload - %q. Available: %s\n", *workloadFlag, allWorkloads)
	case "?":
		fmt.Fprintf(os.Stdout, "Available workloads: %s\n", allWorkloads)
		return nil
	}
}

func main() {
	flag.Parse()

	if err := doMain(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
