KDIR ?= /lib/modules/`uname -r`/build

all: page_alloc_bench.ko page_alloc_bench

page_alloc_bench.ko: page_alloc_bench.kmod.c Kbuild
	$(MAKE) -C $(KDIR) M=$$PWD modules

page_alloc_bench: page_alloc_bench.go
	go build -o $@ page_alloc_bench.go

.PHONY: clean
clean:
	$(RM) page_alloc_bench
	$(MAKE) -C $(KDIR) M=$$PWD clean
