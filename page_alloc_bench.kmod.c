#include <linux/device/class.h>
#include <linux/cdev.h>
#include <linux/fs.h>
#include <linux/module.h>
#include <linux/proc_fs.h>

#define NAME "page_alloc_bench"

static struct proc_dir_entry *procfs_file;

static long page_alloc_bench_ioctl(struct file *filp, unsigned int request, unsigned long argp) {
	return 0;
}

static struct proc_ops proc_ops = {
	.proc_ioctl = page_alloc_bench_ioctl,
	.proc_open = nonseekable_open,
	.proc_lseek = no_llseek,
};

static int __init page_alloc_bench_init(void)
{
	procfs_file = proc_create(NAME, 0, NULL, &proc_ops);

	return 0;
}
module_init(page_alloc_bench_init);

static void __exit page_alloc_bench_exit(void)
{
	proc_remove(procfs_file);
}
module_exit(page_alloc_bench_exit);

MODULE_LICENSE("GPL");