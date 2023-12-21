#include <linux/module.h>

static int __init page_alloc_bench_init(void)
{
	printk("hello world\n");
	return 0;
}
module_init(page_alloc_bench_init);

MODULE_LICENSE("GPL");