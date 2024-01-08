// Command findlimit is what the findlimit workload executes as a subprocess. It
// continuously allocates blocks of memory and prints how many bytes it's
// successully allocated. Presumably it will eventually get OOM-killed. Then you
// can check the last number it printed.
package main

import (
	"flag"
	"fmt"
	"os"
	"syscall"
)

var allocSizeFlag = flag.Int("alloc-size", 4096, "size in bytes of blocks to allocate in")

func doMain() error {
	// Ensure that this process is always the one killed by the OOM killer
	// (assuming nobody else in the system has this oom_score_adj). This lets us
	// allocate memory extremely agressively without worrying about the main
	// benchmark process or sshd or whatever suddenly disappearing.
	if err := os.WriteFile("/proc/self/oom_score_adj", []byte("1000"), 0); err != nil {
		return err
	}

	var allocedBytes int
	for {
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
			allocedBytes += os.Getpagesize()
			fmt.Printf("%d\n", allocedBytes)
		}
	}
}

func main() {
	flag.Parse()

	if err := doMain(); err != nil {
		fmt.Fprintf(os.Stderr, "findlimit workload child: %v\n", err)
		os.Exit(1)
	}
}
