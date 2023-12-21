#include <linux/cdev.h>
#include <linux/fs.h>
#include <linux/module.h>

dev_t dev;
const int minor_devnum = 1;

static int __init page_alloc_bench_init(void)
{
	int err;

	err = alloc_chrdev_region(&dev, 0, minor_devnum, "page_alloc_bench");
	if (err) {
		printk("alloc_chardev_region: %d\n", err);
		return err;
	}

	return 0;
}
module_init(page_alloc_bench_init);

static void __exit page_alloc_bench_exit(void)
{
	unregister_chrdev_region(dev, minor_devnum);
	return;
}
module_exit(page_alloc_bench_exit);

MODULE_LICENSE("GPL");