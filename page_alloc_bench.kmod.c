#include <linux/device/class.h>
#include <linux/cdev.h>
#include <linux/fs.h>
#include <linux/module.h>
#include <linux/proc_fs.h>

#define NAME "pab"

static long pab_ioctl(struct file *filp, unsigned int request, unsigned long argp) {
	return 0;
}

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