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

// Command findlimit is what the findlimit workload executes as a subprocess. It
// continuously allocates blocks of memory and prints how many bytes it's
// successully allocated. Presumably it will eventually get OOM-killed. Then you
// can check the last number it printed.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"

	"golang.org/x/sync/errgroup"
)

var allocSizeFlag = flag.Int("alloc-size", 4096, "size in bytes of blocks to allocate in")

var allocedBytes atomic.Int64

func doMain() error {
	// Ensure that this process is always the one killed by the OOM killer
	// (assuming nobody else in the system has this oom_score_adj). This lets us
	// allocate memory extremely agressively without worrying about the main
	// benchmark process or sshd or whatever suddenly disappearing.
	if err := os.WriteFile("/proc/self/oom_score_adj", []byte("1000"), 0); err != nil {
		return err
	}

	var stdoutMutex sync.Mutex

	eg, ctx := errgroup.WithContext(context.Background())
	for i := 0; i < runtime.NumCPU(); i++ {
		eg.Go(func() error {
			for ctx.Err() == nil {
				prot := syscall.PROT_READ | syscall.PROT_WRITE
				flags := syscall.MAP_PRIVATE | syscall.MAP_ANONYMOUS
				data, err := syscall.Mmap(-1, 0, *allocSizeFlag, prot, flags)
				if err != nil {
					return fmt.Errorf("mmap(%d): %v\n", *allocSizeFlag, err)
				}
				// Trigger faults to actually allocate page (or OOM-kill). Note that
				// under THP some of these accesses aren't actually needed.
				for offset := 0; offset < *allocSizeFlag; offset += os.Getpagesize() {
					data[offset] = 0
				}
				allocedBytes.Add(int64(*allocSizeFlag))
				stdoutMutex.Lock()
				fmt.Printf("%d\n", allocedBytes.Load())
				stdoutMutex.Unlock()
			}
			return ctx.Err()
		})
	}
	return eg.Wait()
}

func main() {
	flag.Parse()

	if err := doMain(); err != nil {
		fmt.Fprintf(os.Stderr, "findlimit workload child: %v\n", err)
		os.Exit(1)
	}
}
