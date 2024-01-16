/*
 * Copyright 2024 Google LLC
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

// WARNING: cgo isn't set up to use the headers from $(KDIR) in the Makefile.
// This will use the system headers, so don't include anything fancy.
#include <linux/ioctl.h>

#define PAB_IOCTL_BASE			0x12

struct pab_ioctl_alloc_page {
	struct {
		int order;
	} args;
	struct {
		unsigned long id; /* Opaque ID for the allocated page, used to free. */
		int nid; /* NUMA node ID, or -1. */
		long latency_ns;
	} result;
};
#define PAB_IOCTL_ALLOC_PAGE	_IOWR(PAB_IOCTL_BASE, 1, struct pab_ioctl_alloc_page)

#define PAB_IOCTL_FREE_PAGE 	_IOR(PAB_IOCTL_BASE, 2, struct page *)
