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

// Package linux provides wrappers for linux syscalls.
package linux

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// Note that sched_setaffinity(2) is documenting the libc wrapper not
// the syscall, we don't need to worry about cpu_set_t (although I
// suspect it's equivalent to a raw cpumask anyway).
type CPUMask []uint64

// NewCPUMask creates a CPUMask with the given CPU numbers set.
func NewCPUMask(cpus ...int) CPUMask {
	maxCPU := slices.Max(cpus)
	mask := make([]uint64, (maxCPU/64)+1)
	for cpu, _ := range cpus {
		mask[cpu/64] |= 1 << (cpu % 64)
	}
	return mask
}

// Parses a CPUMask from this format:
// https://docs.kernel.org/core-api/printk-formats.html#bitmap-and-its-derivatives-such-as-cpumask-and-nodemask
func CPUMaskFromString(s string) (CPUMask, error) {
	parts := strings.Split(strings.TrimSpace(s), ",")
	var mask []uint64
	for _, part := range parts {
		from, to, didCut := strings.Cut(part, "-")
		if didCut {
			fromInt, err := strconv.Atoi(from)
			if err != nil {
				return nil, fmt.Errorf("parsing %q (from %q) as int CPU ID: %v", from, part, err)
			}
			toInt, err := strconv.Atoi(to)
			if err != nil {
				return nil, fmt.Errorf("parsing %q (from %q) as int CPU ID: %v", to, part, err)
			}
			for i := fromInt; i <= toInt; i++ {
				mask = append(mask, uint64(i))
			}
		} else {
			cpu, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("parsing %q (from %q) as int CPU ID: %v", cpu, part, err)
			}
			mask = append(mask, uint64(cpu))
		}
	}
	return CPUMask(mask), nil
}

// PIDCallingThread is an argument for SchedSetaffinity.
const PIDCallingThread = 0

// SchedSetaffinity wraps the sched_setaffinity syscall. Use PIDCallingThread
// for the pid argument to set affinity for the current thread.
func SchedSetaffinity(pid int, mask CPUMask) error {
	size := uintptr(8 * len(mask))
	maskData := uintptr(unsafe.Pointer(unsafe.SliceData(mask)))
	_, _, err := syscall.Syscall(syscall.SYS_SCHED_SETAFFINITY, 0, size, maskData)
	if err != 0 {
		return fmt.Errorf("sched_setaffinity(%d, %+v): %v", pid, mask, err)
	}

	return nil
}

// Ioctl wraps the ioctl syscall.
func Ioctl(file *os.File, cmd, arg uintptr) error {
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), cmd, arg)
	if err != 0 {
		return fmt.Errorf("ioctl 0x%x 0x%x on %s: %v\n", cmd, arg, file.Name(), err)
	}
	return nil
}

// For debugging. SYS_GETCPU not in the syscall package. So this only workds on amd64.
func getcpu() (int, error) {
	var cpu int
	_, _, err := syscall.Syscall(309, uintptr(unsafe.Pointer(&cpu)), 0, 0)
	if err != 0 {
		return -1, fmt.Errorf("getcpu: %v", err)
	}
	return cpu, nil
}

var nodeSubdirRegexp = regexp.MustCompile(`node([0-9+])`)

// NUMANodes scans sysfs to find the map of NUMA node IDs to the set of CPUs they contain.
func NUMANodes() (map[int]CPUMask, error) {
	rootDir := "/sys/devices/system/node/"
	nodeDirs, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %v", rootDir, err)
	}
	ret := make(map[int]CPUMask)
	for _, subdir := range nodeDirs {
		m := nodeSubdirRegexp.FindStringSubmatch(subdir.Name())
		if len(m) != 2 {
			continue
		}
		nodeID, err := strconv.Atoi(m[1])
		if err != nil {
			// Impossible™
			log.Fatal("Can't parse %q (from %q) as number: %v", m[1], subdir.Name())
		}
		cpuMaskSpec, err := os.ReadFile(rootDir + subdir.Name() + "/cpulist")
		if err != nil {
			return nil, fmt.Errorf("reading cpulist for node %d: %v", nodeID, err)
		}
		ret[nodeID], err = CPUMaskFromString(string(cpuMaskSpec))
		if err != nil {
			return nil, err
		}
	}
	return ret, nil
}
