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
	AllocSize pab.ByteSize // Optional.
}

type Result struct {
	Allocated pab.ByteSize
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

func Run(ctx context.Context, opts *Options) (*Result, error) {
	myPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("getting executable path: %v\n", err)
	}
	path := filepath.Join(filepath.Dir(myPath), "workload", "findlimit", "child", "child")
	size := opts.AllocSize
	if size == pab.ByteSize(0) {
		size = 128 * pab.Megabyte
	}
	cmd := exec.CommandContext(ctx, path, fmt.Sprintf("--alloc-size=%d", size.Bytes()))
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("setting up stdout pipe: %v\n", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting workload subprocess: %v\n", err)
	}
	lastLine, err := readLastLine(stdout)
	if err != nil {
		return nil, fmt.Errorf("reading workload subprocess output: %v\n", err)
	}
	if err != nil {
		return nil, fmt.Errorf("workload subprocess failed: %v\n", err)
	}
	// We check the exit conditions of the child process before trying to parse
	// the output as an int. Hopefully this will give us a more useful clue if
	// something caused the workload to shut down immediately.
	err = cmd.Wait()
	if err == nil {
		return nil, fmt.Errorf("expected workload subprocess to get OOM-killed, but it succeeded")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return nil, fmt.Errorf("unexpected error waiting for workload subprocess: %v", err)
	}
	// Ideally we'd check that the signal was specifically SIGKILL here. But I
	// dunno how to do that.
	if cmd.ProcessState.Exited() {
		return nil, fmt.Errorf("expected workload subprocessed to be killed by signal, but it exited (status %d)",
			exitErr.ExitCode())
	}
	numBytes, err := strconv.ParseInt(strings.TrimSpace(lastLine), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parsing last line of workload subprocess output (%q) as int: %v\n",
			lastLine, err)
	}
	return &Result{pab.ByteSize(numBytes)}, nil
}
