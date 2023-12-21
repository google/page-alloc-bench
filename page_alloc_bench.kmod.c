#include <linux/device/class.h>
#include <linux/cdev.h>
#include <linux/fs.h>
#include <linux/module.h>

#define NAME "page_alloc_bench"

dev_t dev;
const int minor_devnum = 1;
static struct cdev cdev;

static struct file_operations fops = {};

static int __init page_alloc_bench_init(void)
{
	int err;

	err = alloc_chrdev_region(&dev, 0, minor_devnum, NAME);
	if (err) {
		printk("alloc_chardev_region: %d\n", err);
		return err;
	}

	cdev_init(&cdev, &fops);
	err = cdev_add(&cdev, dev, minor_devnum);
	if (err)
		goto error;

	return 0;

error:
	if (dev)
		unregister_chrdev_region(dev, minor_devnum);
	return err;
}
module_init(page_alloc_bench_init);

static void __exit page_alloc_bench_exit(void)
{
	cdev_del(&cdev);
	unregister_chrdev_region(dev, minor_devnum);
	return;
}
module_exit(page_alloc_bench_exit);

MODULE_LICENSE("GPL");