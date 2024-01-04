KDIR ?= /lib/modules/`uname -r`/build

all: userspace/page_alloc_bench kmod/page_alloc_bench.ko

.makeself-build/%: %
	@mkdir -p $$(dirname $@)
	-@rm $@
	ln $< $@

# Note the `./` before run.sh is necessary because that also becomes the command
# that the embedded script runs, so without it this won't work on systems where
# '.' is not in $PATH.
page_alloc_bench.run: $(addprefix .makeself-build/, run.sh userspace/page_alloc_bench kmod/page_alloc_bench.ko)
	makeself .makeself-build page_alloc_bench.run "page_alloc_bench" ./run.sh

kmod/page_alloc_bench.ko: $(addprefix kmod/, page_alloc_bench.c Kbuild)
	$(MAKE) -C $(KDIR) M=$$PWD/kmod modules

# Separate optional target since it will to build unless KDIR has been set to a
# full kernel tree.
kmod/compile_commands.json: kmod/page_alloc_bench.ko
	$(MAKE) -C $(KDIR) M=$$PWD/kmod compile_commands.json

.PHONY: userspace/page_alloc_bench
userspace/page_alloc_bench:
	cd userspace; go build .

.PHONY: clean
clean:
	$(RM) userspace/page_alloc_bench
	$(MAKE) -C $(KDIR) M=$$PWD/kmod clean
	$(RM) -rf .makeself-build
	$(RM) page_alloc_bench.run
