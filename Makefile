KDIR ?= /lib/modules/`uname -r`/build

all: page_alloc_bench page_alloc_bench.ko

# Note the `./` before run.sh is necessary because that also becomes the command
# that the embedded script runs, so without it this won't work on systems where
# '.' is not in $PATH.
page_alloc_bench.run: run.sh page_alloc_bench page_alloc_bench.ko
	makeself --tar-extra "--exclude=.git" $$PWD page_alloc_bench.run "page_alloc_bench" ./run.sh

page_alloc_bench.ko: page_alloc_bench.kmod.c Kbuild
	$(MAKE) -C $(KDIR) M=$$PWD modules

page_alloc_bench: page_alloc_bench.go
	go build -o $@ page_alloc_bench.go

.PHONY: clean
clean:
	$(RM) page_alloc_bench
	$(MAKE) -C $(KDIR) M=$$PWD clean
	$(RM) page_alloc_bench.run
