// WARNING: cgo isn't set up to use the headers from $(KDIR) in the Makefile.
// This will use the system headers, so don't include anything fancy.
#include <linux/ioctl.h>

#define PAB_IOCTL_BASE			0x12
#define PAB_IOCTL_ALLOC_PAGE	_IOW(PAB_IOCTL_BASE, 1, struct page *)
#define PAB_IOCTL_FREE_PAGE 	_IOR(PAB_IOCTL_BASE, 2, struct page *)
