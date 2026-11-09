import { goto, invalidate } from '$app/navigation';
import { SvelteMap } from 'svelte/reactivity';
import { api, ApiError } from './api/client.ts';
import type { Item, Subscriptions } from './api/types.ts';

const STORAGE_KEY = 'fever.pendingReads';
const FLUSH_DELAY = 2000;
const MAX_RETRY_DELAY = 60_000;
const CHUNK = 1000; // the server's limit per request
// While reading, the local adjustments keep counts right: the server's
// counts only need reloading now and then.
const COUNTS_RELOAD_EVERY = 15_000;

interface Pending {
	read: number[];
	unread: number[];
}

/** A change to unread counts that the loaded subscriptions do not show yet. */
interface CountChange {
	feedId: number;
	delta: number; // net change for this item: -1 read, +1 unread, 0 back to start
	version: number;
}

/**
 * Read state changes made on this device. They show at once, are kept in
 * local storage until the server has them (so a closed tab or a lost
 * connection loses nothing), and are sent in batches.
 */
class ItemState {
	#read = new Set<number>();
	#unread = new Set<number>();
	#version = 0;
	#timer: ReturnType<typeof setTimeout> | undefined;
	#busy = false;
	#retryDelay = FLUSH_DELAY;
	#confirmed = 0; // highest change version the server has
	#countsTimer: ReturnType<typeof setTimeout> | undefined;
	#lastCountsReload = 0;

	/** Per item: how the unread counts differ from the loaded subscriptions. */
	counts = new SvelteMap<number, CountChange>();

	constructor() {
		if (typeof window === 'undefined') return;
		try {
			const saved = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? 'null') as Pending | null;
			saved?.read?.forEach((id) => this.#read.add(id));
			saved?.unread?.forEach((id) => this.#unread.add(id));
		} catch {
			// nothing saved, or storage unavailable
		}
		// Leaving or hiding the page (switching apps on a phone): send what
		// is pending right away. It stays stored until a normal send succeeds.
		const sendNow = () => this.#sendBeacon();
		addEventListener('pagehide', sendNow);
		document.addEventListener('visibilitychange', () => {
			if (document.visibilityState === 'hidden') sendNow();
			else this.#schedule(0);
		});
		this.#schedule(0);
	}

	markRead(item: Item) {
		if (item.read) return;
		item.read = true;
		this.#unread.delete(item.id);
		this.#read.add(item.id);
		this.#changed(item, -1);
	}

	markUnread(item: Item) {
		if (!item.read) return;
		item.read = false;
		this.#read.delete(item.id);
		this.#unread.add(item.id);
		this.#changed(item, +1);
	}

	/** Items whose state here may differ from what the server last said. */
	isPending(id: number): boolean {
		return this.#read.has(id) || this.#unread.has(id) || this.counts.has(id);
	}

	/** Saving is rare and explicit, so it is sent right away. */
	async setSaved(item: Item, saved: boolean) {
		if (item.saved === saved) return;
		item.saved = saved;
		try {
			await api('/api/items/state', {
				method: 'POST',
				body: JSON.stringify({ ids: [item.id], saved })
			});
			await this.reloadCounts();
		} catch (err) {
			item.saved = !saved;
			throw err;
		}
	}

	/**
	 * Reloads the subscriptions (unread and saved counts). Every reload goes
	 * through here: the local adjustments the server confirmed before the
	 * reload started are then included in its counts, and are dropped.
	 */
	async reloadCounts() {
		const confirmed = this.#confirmed;
		this.#lastCountsReload = Date.now();
		await invalidate('app:subscriptions');
		for (const [id, change] of this.counts) {
			if (change.version <= confirmed) this.counts.delete(id);
		}
	}

	/** Sends pending changes now, e.g. right after logging in again. */
	sendSoon() {
		clearTimeout(this.#timer);
		this.#timer = undefined;
		this.#retryDelay = FLUSH_DELAY;
		this.#schedule(0);
	}

	#reloadCountsSoon() {
		if (this.#countsTimer !== undefined) return;
		const wait = Math.max(0, this.#lastCountsReload + COUNTS_RELOAD_EVERY - Date.now());
		this.#countsTimer = setTimeout(() => {
			this.#countsTimer = undefined;
			void this.reloadCounts();
		}, wait);
	}

	#changed(item: Item, delta: number) {
		const prev = this.counts.get(item.id);
		this.counts.set(item.id, {
			feedId: item.feedId,
			delta: (prev?.delta ?? 0) + delta,
			version: ++this.#version
		});
		this.#store();
		this.#schedule(FLUSH_DELAY);
	}

	#store() {
		try {
			const pending: Pending = { read: [...this.#read], unread: [...this.#unread] };
			if (pending.read.length || pending.unread.length) {
				localStorage.setItem(STORAGE_KEY, JSON.stringify(pending));
			} else {
				localStorage.removeItem(STORAGE_KEY);
			}
		} catch {
			// storage unavailable: changes are still sent while the page is open
		}
	}

	#schedule(delay: number) {
		if (this.#timer !== undefined) return;
		this.#timer = setTimeout(() => {
			this.#timer = undefined;
			void this.#flush();
		}, delay);
	}

	async #flush() {
		if (this.#busy || (!this.#read.size && !this.#unread.size)) return;
		this.#busy = true;
		const read = [...this.#read];
		const unread = [...this.#unread];
		const version = this.#version;
		try {
			for (const [ids, isRead] of [
				[read, true],
				[unread, false]
			] as const) {
				for (let i = 0; i < ids.length; i += CHUNK) {
					await api('/api/items/state', {
						method: 'POST',
						body: JSON.stringify({ ids: ids.slice(i, i + CHUNK), read: isRead })
					});
				}
			}
			// Sent: forget them, unless they changed again meanwhile.
			read.forEach((id) => this.#unread.has(id) || this.#read.delete(id));
			unread.forEach((id) => this.#read.has(id) || this.#unread.delete(id));
			this.#store();
			this.#retryDelay = FLUSH_DELAY;
			this.#confirmed = Math.max(this.#confirmed, version);
			this.#reloadCountsSoon();
		} catch (err) {
			// Kept in storage and tried again, with growing delays. After a
			// 401 the session has ended: the login page sends them as soon
			// as the reader is back (sendSoon).
			this.#retryDelay = Math.min(this.#retryDelay * 2, MAX_RETRY_DELAY);
			this.#busy = false;
			this.#schedule(this.#retryDelay);
			if (err instanceof ApiError && err.status === 401 && location.pathname !== '/login') {
				await goto('/login', { refreshAll: true });
			}
			return;
		} finally {
			this.#busy = false;
		}
		if (this.#read.size || this.#unread.size) this.#schedule(FLUSH_DELAY);
	}

	/** Best-effort send that survives the page closing. */
	#sendBeacon() {
		for (const [ids, read] of [
			[[...this.#read], true],
			[[...this.#unread], false]
		] as const) {
			for (let i = 0; i < ids.length; i += CHUNK) {
				fetch('/api/items/state', {
					method: 'POST',
					keepalive: true,
					credentials: 'same-origin',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ ids: ids.slice(i, i + CHUNK), read })
				}).catch(() => {
					// sent again by the next normal flush
				});
			}
		}
	}
}

export const itemState = new ItemState();

/** The subscriptions with this device's unsent or unreloaded read changes applied. */
export function withLocalCounts(
	subs: Subscriptions,
	counts: SvelteMap<number, CountChange>
): Subscriptions {
	if (counts.size === 0) return subs;
	const byFeed = new Map<number, number>();
	for (const { feedId, delta } of counts.values()) {
		byFeed.set(feedId, (byFeed.get(feedId) ?? 0) + delta);
	}
	const feeds = subs.feeds.map((f) =>
		byFeed.has(f.id) ? { ...f, unread: Math.max(0, f.unread + (byFeed.get(f.id) ?? 0)) } : f
	);
	const unreadOf = new Map(feeds.map((f) => [f.id, f.unread]));
	const groups = subs.groups.map((g) => ({
		...g,
		unread: g.feedIds.reduce((n, id) => n + (unreadOf.get(id) ?? 0), 0)
	}));
	const totalUnread = feeds.reduce((n, f) => n + f.unread, 0);
	return { ...subs, feeds, groups, totalUnread };
}
