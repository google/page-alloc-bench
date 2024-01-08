// Package findlimit is the core package for the workload that tries to find out
// how much memory the system can allocate.
package findlimit

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/page_alloc_bench/pab"
)

type Options struct {
	AllocSize pab.ByteSize
}

func readLastLine(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	var line string
	for scanner.Scan() {
		line = scanner.Text()
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return line, nil
}

func Run(ctx context.Context, opts *Options) error {
	myPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting executable path: %v\n", err)
	}
	path := filepath.Join(filepath.Dir(myPath), "workload", "findlimit", "child", "child")
	cmd := exec.CommandContext(ctx, path, fmt.Sprintf("--alloc-size=%d", opts.AllocSize.Bytes()))
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("setting up stdout pipe: %v\n", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting workload subprocess: %v\n", err)
	}
	lastLine, err := readLastLine(stdout)
	if err != nil {
		return fmt.Errorf("reading workload subprocess output: %v\n", err)
	}
	if err != nil {
		return fmt.Errorf("workload subprocess failed: %v\n", err)
	}
	numBytes, err := strconv.ParseInt(strings.TrimSpace(lastLine), 10, 64)
	if err != nil {
		return fmt.Errorf("parsing last line of workload subprocess output (%q) as int: %v\n",
			lastLine, err)
	}
	err = cmd.Wait()
	if err == nil {
		return fmt.Errorf("expected workload subprocess to get OOM-killed, but it succeeded")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return fmt.Errorf("unexpected error waiting for workload subprocess: %v", err)
	}
	// Ideally we'd check that the signal was specifically SIGKILL here. But I
	// dunno how to do that.
	if cmd.ProcessState.Exited() {
		return fmt.Errorf("expected workload subprocessed to be killed by signal, but it exited (status %d)",
			exitErr.ExitCode())
	}
	fmt.Printf("Allocated %s total (mmap granularity %s)\n",
		pab.ByteSize(numBytes).String(), opts.AllocSize.String())
	return nil
}
