#include <linux/cdev.h>
#include <linux/fs.h>
#include <linux/module.h>
#include <linux/proc_fs.h>
#include <linux/uaccess.h>

#define NAME "page_alloc_bench"

/* IDs need to be manually synced with the Go file */
#define PAB_IOCTL_ALLOC_PAGE	0x12340001
#define PAB_IOCTL_FREE_PAGE 	0x12340002

static long pab_ioctl(struct file *file, unsigned int cmd, unsigned long arg) {
	switch (cmd) {

		case PAB_IOCTL_ALLOC_PAGE: {
			struct page *page;

			page = alloc_page(GFP_KERNEL);

			if (!page)
				return -ENOMEM;

			return put_user(page, (struct page **)arg);
		}
		case PAB_IOCTL_FREE_PAGE: {
			struct page *page = (struct page *)arg;

			__free_page(page);
			return 0;
		}
		default:
			pr_err("Invalid page_alloc_bench ioctl 0x%x\n", cmd);
			return -EINVAL;
	}
}

/* Procfs is a pretty convenient way to create a file where we can receive ioctls. */
static struct proc_ops proc_ops = {
	.proc_ioctl = pab_ioctl,
	.proc_open = nonseekable_open,
	.proc_lseek = no_llseek,
};

static struct proc_dir_entry *procfs_file;

static int __init pab_init(void)
{
	procfs_file = proc_create(NAME, 0, NULL, &proc_ops);

	return 0;
}
module_init(pab_init);

static void __exit pab_exit(void)
{
	proc_remove(procfs_file);
}
module_exit(pab_exit);

MODULE_LICENSE("GPL");