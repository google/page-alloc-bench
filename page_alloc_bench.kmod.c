#include <linux/module.h>

static int __init page_alloc_bench_init(void)
{
	printk("hello world\n");
	return 0;
}
module_init(page_alloc_bench_init);

static void __exit page_alloc_bench_exit(void)
{
	printk("good buy world\n");
	return;
}
module_exit(page_alloc_bench_exit);

MODULE_LICENSE("GPL");