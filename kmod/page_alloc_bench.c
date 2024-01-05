/*
 * Copyright 2023 Google LLC
 *
 * This program is free software; you can redistribute it and/or
 * modify it under the terms of the GNU General Public License
 * as published by the Free Software Foundation; either version 2
 * of the License, or (at your option) any later version.

 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.

 * You should have received a copy of the GNU General Public License
 * along with this program; If not, see <http://www.gnu.org/licenses/>.
 */

#include <linux/cdev.h>
#include <linux/fs.h>
#include <linux/module.h>
#include <linux/proc_fs.h>
#include <linux/uaccess.h>

#define NAME "page_alloc_bench"

/* IDs need to be manually synced with the Go file */
#define PAB_IOCTL_ALLOC_PAGE	0x12340001
#define PAB_IOCTL_FREE_PAGE 	0x12340002

/*
 * So we don't leak pages if userspace crashes, store them on a list. They're
 * per-CPU since that at least eliminates contention except when freeing remote
 * pages.
 */
struct alloced_pages {
	spinlock_t lock;
	struct list_head pages;
};
static DEFINE_PER_CPU(struct alloced_pages, alloced_pages);

static void alloced_pages_init(void)
{
	int cpu;

	for_each_possible_cpu(cpu) {
		struct alloced_pages *ap = per_cpu_ptr(&alloced_pages, cpu);

		spin_lock_init(&ap->lock);
		INIT_LIST_HEAD(&ap->pages);
	}
}

static void alloced_pages_store(struct page *page)
{
	struct alloced_pages *ap;

	get_cpu();

	ap = this_cpu_ptr(&alloced_pages);
	page->private = (unsigned long)ap;
	spin_lock(&ap->lock);
	list_add(&page->lru, &ap->pages);
	spin_unlock(&ap->lock);

	put_cpu();
}

static void alloced_pages_forget(struct page *page)
{
	struct alloced_pages *ap = (struct alloced_pages *)page->private;

	spin_lock(&ap->lock);
	list_del(&page->lru);
	spin_unlock(&ap->lock);
}

static void alloced_pages_free_all(void)
{
	int cpu;

	for_each_possible_cpu(cpu) {
		struct alloced_pages *ap = per_cpu_ptr(&alloced_pages, cpu);
		struct page *page, *tmp;

		/* Contention should be impossible at this point, and isn't handled. */
		WARN_ON(spin_is_locked(&ap->lock));

		list_for_each_entry_safe(page, tmp, &ap->pages, lru) {
			WARN_ON(page->private != (unsigned long)ap);
			list_del(&page->lru);
			__free_page(page);
		}
	}
}

static long pab_ioctl(struct file *file, unsigned int cmd, unsigned long arg) {
	switch (cmd) {

		case PAB_IOCTL_ALLOC_PAGE: {
			struct page *page;

			page = alloc_page(GFP_KERNEL);
			if (!page)
				return -ENOMEM;

			alloced_pages_store(page);

			return put_user(page, (struct page **)arg);
		}
		case PAB_IOCTL_FREE_PAGE: {
			struct page *page = (struct page *)arg;

			alloced_pages_forget(page);

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
	alloced_pages_init();

	procfs_file = proc_create(NAME, 0, NULL, &proc_ops);

	return 0;
}
module_init(pab_init);

static void __exit pab_exit(void)
{
	proc_remove(procfs_file);

	alloced_pages_free_all();
}
module_exit(pab_exit);

MODULE_LICENSE("GPL");