tl;dr:

```sh
sudo apt install go build-essential makeself
make KDIR=$KERNEL_TREE page_alloc_bench.run
scp page_alloc_bench.run $HOST:
ssh $HOST sudo ./page_alloc_bench.run
```

Dependencies:

 - Kernel build toolchain.
 - Go toolchain.
 - Optional: [makeself](http://makeself.io).

On Debian-alikes try `sudo apt install go build-essential makeself`.

If you like to live dangerously, build against your current kernel with just
`make`. Then `sudo ./run.sh` to run the thing.

To run on another system, use `make KDIR=$KERNEL_TREE`. If that kernel was built
with clang add `LLVM=1`. In theory I think you should only have to have done
`make modules prepare` in your kernel tree but in practice I've found you
sometimes have to completely build the kernel for this to work (this issue might
be unique to Google's kernel fork though).

When you're using a full kernel tree via `KDIR` you can also build the
`compile_commands.json` target to make clangd work on the kmod code.

You can build the `page_alloc_bench.run` target to build a self-extracting
binary with `makeself`. This isn't in the default target, you need to explicitly
list it as the make goal. You can then copy this onto some other system, running
the kernel from your `$KERNEL_TREE` and just execute it. This is optional, you
can also just copy all the relevant files manually and run `run.sh` directly.