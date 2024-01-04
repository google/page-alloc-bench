#define PAB_IOCTL_BASE			0x12
/* IDs need to be manually synced with the Go file */
#define PAB_IOCTL_ALLOC_PAGE	_IOW(PAB_IOCTL_BASE, 1, struct page *)
#define PAB_IOCTL_FREE_PAGE 	_IOR(PAB_IOCTL_BASE, 2, struct page *)
