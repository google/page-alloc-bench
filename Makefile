KDIR ?= /lib/modules/`uname -r`/build

all: page_alloc_bench page_alloc_bench.ko

.makeself-build/%: %
	@mkdir -p $$(dirname $@)
	-@rm $@
	ln $< $@

# Note the `./` before run.sh is necessary because that also becomes the command
# that the embedded script runs, so without it this won't work on systems where
# '.' is not in $PATH.
page_alloc_bench.run: .makeself-build/run.sh .makeself-build/page_alloc_bench .makeself-build/page_alloc_bench.ko
	makeself .makeself-build page_alloc_bench.run "page_alloc_bench" ./run.sh

page_alloc_bench.ko: page_alloc_bench.kmod.c Kbuild
	$(MAKE) -C $(KDIR) M=$$PWD modules

# Separate optional target since it will to build unless KDIR has been set to a
# full kernel tree.
compile_commands.json: page_alloc_bench.ko
	$(MAKE) -C $(KDIR) M=$$PWD

page_alloc_bench: page_alloc_bench.go
	go build -o $@ page_alloc_bench.go

.PHONY: clean
clean:
	$(RM) page_alloc_bench
	$(MAKE) -C $(KDIR) M=$$PWD clean
	$(RM) -rf .makeself-build
	$(RM) page_alloc_bench.run
