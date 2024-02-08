# tl;dr

```sh
sudo apt install go build-essential makeself
make KDIR=$KERNEL_TREE page_alloc_bench.run
scp page_alloc_bench.run $HOST:
ssh $HOST sudo ./page_alloc_bench.run
```

# Build and run

Dependencies:

 - Kernel build toolchain.
 - Go toolchain.
 - Optional: [makeself](http://makeself.io).

On Debian-alikes try `sudo apt install go build-essential makeself`.

If you like to live dangerously, build against your current kernel with just
`make`. Then see `./run.sh --help` to run the thing.

To run on another system, use `make KDIR=$KERNEL_TREE`. If that kernel was built
with clang add `LLVM=1`. In theory I think you should only have to have done
`make modules prepare` in your kernel tree but in practice I've found you
sometimes have to completely build the kernel for this to work (this issue might
be unique to Google's kernel fork though). Cross-compilation for other arches
isn't supported (I probably shouldn't have used Go...).

When you're using a full kernel tree via `KDIR` you can also build the
`kmod/compile_commands.json` target to make clangd work on the kmod code.

You can build the `page_alloc_bench.run` target to build a self-extracting
binary with `makeself`. This isn't in the default target, you need to explicitly
list it as the make goal. You can then copy this onto some other system, running
the kernel from your `$KERNEL_TREE` and just execute it. This is optional, you
can also just copy all the relevant files manually and run `run.sh` directly.

# Output

You can pass `--output-path`, data measured by the workload will be written
there as JSON. This currently only has one field:

- `idle_available_bytes`: This workload attempts to allocate as much memory as
  possible from userspace. It then does this again while simultaneously
  allocating then freeing kernel pages on all CPUs. This metric reports how much
  it was able to allocate the second time. You don't want this number to go
  down. The allocation is repeated, this field has one item for each iteration.
- `antagonized_available_bytes`: This is like `idle_available_bytes`, but it's
  measured while an antagonistic kernel allocation workload runs in the
  background.
- `kernel_page_allocs`: Total number of pages the antagonistic kernel workers
  could allocate
- `kernel_alloc_failures`: Number of times the kernel workers failed to allocate
  a page. Allocations are performed with expontential backoff so it's likely the
  only relevant aspect of this metric is whether it's zero or nonzero. If
  nonzero, perhaps something is wrong and the other metrics should be eyed with
  suspicion.
- `kernel_page_allocs_remote`: Of the above, the number of pages that came from
  a remote NUMA node.
- `kernel_page_alloc_latencies_ns`: Sample of latencies for the kernel
  allocation call. This is just the last N samples, no proper sampling logic is
  implemented.

If you set `--alloc-orders` to contain multiple values (this is the default),
the benchmark is repeated for each of the listed orders. The order is used as
the argument to alloc_pages in the kernel-allocation aspect of the workload
(i.e. we allocate pages of size 2^order), but doesn't influence the userspace
allocation part.