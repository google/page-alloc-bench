Build against your current kernel with just `make`.

Use `make KDIR=$KERNEL_TREE` to build against another kernel. If that kernel was
built with clang add `LLVM=1`. In theory I think you should only have to have
done `make modules prepare` in your kernel tree but in practice I've found you
sometimes have to completely build the kernel for this to work (this issue might
be unique to Google's kernel fork though).