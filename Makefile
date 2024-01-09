KDIR ?= /lib/modules/`uname -r`/build

go_binaries = userspace/page_alloc_bench userspace/workload/findlimit/child/child
all: $(go_binaries) kmod/page_alloc_bench.ko

.makeself-build/%: %
	@mkdir -p $$(dirname $@)
	-@rm $@
	ln $< $@

# Note the `./` before run.sh is necessary because that also becomes the command
# that the embedded script runs, so without it this won't work on systems where
# '.' is not in $PATH.
page_alloc_bench.run: $(addprefix .makeself-build/, run.sh kmod/page_alloc_bench.ko $(go_binaries))
	makeself .makeself-build page_alloc_bench.run "page_alloc_bench" ./run.sh

.PHONY: kmod/page_alloc_bench.ko # Let kbuild decide the dependencies.
kmod/page_alloc_bench.ko:
	$(MAKE) -C $(KDIR) M=$$PWD/kmod modules

# Separate optional target since it will not build unless KDIR has been set to a
# full kernel tree.
kmod/compile_commands.json: kmod/page_alloc_bench.ko
	$(MAKE) -C $(KDIR) M=$$PWD/kmod compile_commands.json

.PHONY: $(go_binaries) # Let Go decide the dependencies.
$(go_binaries):
	cd $$(dirname $@); go build .

.PHONY: clean
clean:
	$(RM) $(go_binaries)
	$(MAKE) -C $(KDIR) M=$$PWD/kmod clean
	$(RM) -rf .makeself-build
	$(RM) page_alloc_bench.run
