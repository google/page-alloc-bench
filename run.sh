#!/bin/sh

set -eu

insmod page_alloc_bench.ko

do_rmmod() {
        rmmod page_alloc_bench.ko
}

trap do_rmmod EXIT
./page_alloc_bench
