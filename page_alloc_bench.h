// WARNING: cgo isn't set up to use the headers from $(KDIR) in the Makefile.
// This will use the system headers, so don't include anything fancy.
#include <linux/ioctl.h>

#define PAB_IOCTL_BASE			0x12

struct pab_ioctl_alloc_page {
	struct {
		int order;
	} args;
	unsigned long result; /* Opaque ID */
};
#define PAB_IOCTL_ALLOC_PAGE	_IOWR(PAB_IOCTL_BASE, 1, struct pab_ioctl_alloc_page)

#define PAB_IOCTL_FREE_PAGE 	_IOR(PAB_IOCTL_BASE, 2, struct page *)
