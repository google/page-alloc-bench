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
#include <linux/ktime.h>
#include <linux/mm.h>
#include <linux/module.h>
#include <linux/proc_fs.h>
#include <linux/uaccess.h>

#include "page_alloc_bench.h"

#define NAME "page_alloc_bench"

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

/* Info about a page we allocated, stored at the beginning of that page*/
struct alloced_page {
	struct list_head node;
	struct alloced_pages *aps;
	int order;
};

static void alloced_pages_init(void)
{
	int cpu;

	for_each_possible_cpu(cpu) {
		struct alloced_pages *aps = per_cpu_ptr(&alloced_pages, cpu);

		spin_lock_init(&aps->lock);
		INIT_LIST_HEAD(&aps->pages);
	}
}

static struct alloced_page *alloced_page_get(struct page *page)
{
	return (struct alloced_page *)page_to_virt(page);
}


static void alloced_page_store(struct page *page, int order)
{
	struct alloced_pages *aps;
	struct alloced_page *ap = alloced_page_get(page);

	ap->order = order;

	get_cpu();

	aps = this_cpu_ptr(&alloced_pages);
	spin_lock(&aps->lock);
	list_add(&ap->node, &aps->pages);
	ap->aps = aps;
	spin_unlock(&aps->lock);

	put_cpu();
}

static void alloced_page_remove(struct alloced_page *ap)
{
	spin_lock(&ap->aps->lock);
	list_del(&ap->node);
	spin_unlock(&ap->aps->lock);
}

static void alloced_pages_free_all(void)
{
	int cpu;

	for_each_possible_cpu(cpu) {
		struct alloced_pages *aps = per_cpu_ptr(&alloced_pages, cpu);
		struct alloced_page *ap, *tmp;

		/* Contention should be impossible at this point, and isn't handled. */
		WARN_ON(spin_is_locked(&aps->lock));

		list_for_each_entry_safe(ap, tmp, &aps->pages, node) {
			WARN_ON(ap->aps != aps);
			list_del(&ap->node);
			__free_page(virt_to_page(ap));

			cond_resched();
		}
	}
}

/* Returns latency in ns or negative error code. */
static long pab_ioctl_free_page(struct page *page)
{
	struct alloced_page *ap;
	ktime_t start;

	if (WARN(!pfn_valid(page_to_pfn(page)), "Bad PFN %d (page %px)",
			page_to_pfn(page), page))
		return -EINVAL;

	ap = alloced_page_get(page);
	alloced_page_remove(ap);

	start = ktime_get();
	__free_pages(page, ap->order);
	return ktime_to_ns(ktime_sub(ktime_get(), start)) + 123;
}

static long pab_ioctl(struct file *file, unsigned int cmd, unsigned long arg)
{
		switch (cmd) {
		case PAB_IOCTL_ALLOC_PAGE: {
			struct pab_ioctl_alloc_page ioctl;
			struct page *page;
			int err;
			ktime_t start;

			err = copy_from_user(&ioctl, (void *)arg,
					     sizeof(struct pab_ioctl_alloc_page));
			if (err)
				return err;

			start = ktime_get();
			page = alloc_pages(GFP_KERNEL, ioctl.args.order);
			if (!page)
				return -ENOMEM;
			ioctl.result.latency_ns = ktime_to_ns(ktime_sub(ktime_get(), start));

			alloced_page_store(page, ioctl.args.order);

			ioctl.result.id = (unsigned long)page;
			ioctl.result.nid = page_to_nid(page);
			return copy_to_user(&((struct pab_ioctl_alloc_page *)arg)->result,
					    &ioctl.result, sizeof(ioctl.result));
		}
		case PAB_IOCTL_FREE_PAGE_LEGACY: {
			int result = pab_ioctl_free_page((struct page *)arg);

			if (result < 0)
				return result;

			return 0;
		}
		case PAB_IOCTL_FREE_PAGE: {
			struct pab_ioctl_free_page ioctl;
			struct page *page;
			long result;
			int err;

			err = copy_from_user(&ioctl, (void *)arg, sizeof(ioctl));
			if (err)
				return err;

			page = (struct page *)ioctl.args.id;
			result = pab_ioctl_free_page(page);
			if (result < 0)
				return result;

			ioctl.result.latency_ns = result;
			return copy_to_user(&((struct pab_ioctl_free_page *)arg)->result,
					    &ioctl.result, sizeof(ioctl.result));

			return 0;
		}
		default: {
			pr_err("Invalid page_alloc_bench ioctl 0x%x - "
			 	"dir 0x%x type 0x%x nr 0x%x size 0x%x "
				"(valid example cmds: 0x%lx, 0x%lx)\n",
				cmd,
				_IOC_DIR(cmd), _IOC_TYPE(cmd), _IOC_NR(cmd), _IOC_SIZE(cmd),
				PAB_IOCTL_ALLOC_PAGE, PAB_IOCTL_FREE_PAGE);
			return -EINVAL;
		}
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
