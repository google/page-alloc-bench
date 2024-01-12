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
	"flag"
	"fmt"
	"log"
	"math/bits"
	"os"
	"runtime"
	"sync"
	"syscall"

	"github.com/google/page_alloc_bench/pab"
)

var (
	initAllocSize = flag.Int("init-alloc-size", 0, "Size of initial up-front alloc. Optional.")
	allocSize     = flag.Int("alloc-size", 0, "Size of subsequent individual allocs.")
)

func mmap(size int) ([]byte, error) {
	prot := syscall.PROT_READ | syscall.PROT_WRITE
	flags := syscall.MAP_PRIVATE | syscall.MAP_ANONYMOUS
	return syscall.Mmap(-1, 0, size, prot, flags)
}

func doMain() error {
	// Ensure that this process is always the one killed by the OOM killer
	// (assuming nobody else in the system has this oom_score_adj). This lets us
	// allocate memory extremely agressively without worrying about the main
	// benchmark process or sshd or whatever suddenly disappearing.
	if err := os.WriteFile("/proc/self/oom_score_adj", []byte("1000"), 0); err != nil {
		return err
	}

	// Having each goroutine below contend on a mutex to write stdout turned out
	// to be super slow. So we do The Go Thing and collect everything in a
	// separate goroutine.
	allocedBytes := int64(0)
	allocCh := make(chan int64, 4096) // Buffering is just for performance.
	go func() {
		for size := range allocCh {
			allocedBytes += size
			fmt.Printf("%d\n", allocedBytes)
		}
	}()

	for {
		// Make this bigger to reduce the number of syscalls and speed the benchmark
		// up. Make it smaller to make the benchmark work on teeny weeny leedle
		// computers. The code below assumes it's a multiple of the page size.
		const mmapSize = 256 * pab.Megabyte
		data, err := mmap(int(mmapSize.Bytes()))
		if err != nil {
			log.Fatalf("mmap failed. Computer too teeny? /proc/sys/vm/overcommit_memory set to 2? %v", err)
		}

		// Touch pages to actually fault them into memory, this is where the
		// real allocation happens. We'll do this in parallel for speed. We
		// divide the mmaped region into equally sized chunks and run a
		// goroutine per chunk, to make them equally sized we just divide them
		// into a power of two. I can't do maths with other numbers sorry.
		goros := 1 << (63 - bits.LeadingZeros64(uint64(runtime.NumCPU())))
		chunkSize := mmapSize.Bytes() / int64(goros)
		pageSize := int64(os.Getpagesize()) // This is a syscall so just do it once.
		var wg sync.WaitGroup
		for chunkStart := int64(0); chunkStart < mmapSize.Bytes(); chunkStart += chunkSize {
			wg.Add(1)
			go func() {
				for offset := int64(0); offset < chunkSize; offset += pageSize {
					data[chunkStart+offset] = 0
					allocCh <- pageSize
				}
				wg.Done()
			}()
		}

		wg.Wait()
	}
}

func main() {
	flag.Parse()

	if err := doMain(); err != nil {
		fmt.Fprintf(os.Stderr, "findlimit workload child: %v\n", err)
		os.Exit(1)
	}
}
