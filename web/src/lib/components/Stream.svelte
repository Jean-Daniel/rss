<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { invalidate } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '#lib/api/client.ts';
	import type { Item, ItemChanges, ItemPage, Subscriptions } from '#lib/api/types.ts';
	import { clock, onResume } from '#lib/foreground.svelte.ts';
	import { itemState } from '#lib/item-state.svelte.ts';
	import { toLogin } from '#lib/navigation.ts';
	import { setShowReadPref } from '#lib/prefs.ts';
	import { shortcuts } from '#lib/shortcuts.svelte.ts';
	import { itemsURL, type StreamData } from '#lib/stream.ts';
	import { shownSubscriptions } from '#lib/subscriptions.ts';
	import { phone } from '#lib/ui.svelte.ts';
	import ItemView from './ItemView.svelte';

	let { stream, subs }: { stream: StreamData; subs: Subscriptions } = $props();

	// This component is re-created for every list (see the pages' {#key}),
	// so the first page only seeds its state.
	const initial = untrack(() => stream);
	const scope = initial.scope;
	const options = initial.options;
	let items = $state<Item[]>(initial.page.items);
	let next = $state(initial.page.next);
	let loading = $state(false);
	let loadError = $state('');
	// When the latest page was loaded: "mark all as read" covers what had
	// arrived by then, including items shown by later pages.
	let shownAt = initial.page.loadedAt;

	// The feeds as loaded, for the items' titles and icons: they only change
	// when the subscriptions are reloaded. The unread count comes from the
	// subscriptions as the reader shows them, which change with every read.
	const feedsById = $derived(new Map(subs.feeds.map((f) => [f.id, f])));
	const shown = shownSubscriptions();
	const unread = $derived.by(() => {
		switch (scope.kind) {
			case 'all':
				return shown().totalUnread;
			case 'group':
				return shown().groups.find((g) => g.id === scope.id)?.unread ?? 0;
			case 'feed':
				return shown().feeds.find((f) => f.id === scope.id)?.unread ?? 0;
			default:
				return 0;
		}
	});

	// Links to a feed keep the group the reader is in.
	const groupId = $derived(
		scope.kind === 'group' ? scope.id : Number(page.url.searchParams.get('group')) || null
	);
	const feedHref = (feedId: number) =>
		groupId ? `/feeds/${feedId}?group=${groupId}` : `/feeds/${feedId}`;

	/** Loads the next page; false when there was nothing to do. */
	async function loadMore(): Promise<boolean> {
		if (loading || !next) return false;
		loading = true;
		loadError = '';
		try {
			const more = await api<ItemPage>(itemsURL(scope, options, next));
			const known = new Set(items.map((it) => it.id));
			items.push(...more.items.filter((it) => !known.has(it.id)));
			next = more.next;
			shownAt = more.loadedAt;
		} catch (err) {
			loadError = err instanceof Error ? err.message : String(err);
		} finally {
			loading = false;
		}
		return true;
	}

	// Mark as read on scroll: an item is read once its bottom edge has gone
	// above the top of the reading area (below the phone's top bar).
	// Items the reader marked unread here are left alone.
	//
	// Only items seen in or below the view count: after a reload or a back
	// navigation the browser restores the old scroll position over a
	// different list, and the items above it were never on screen.
	const keepUnread = new Set<number>();
	const itemOf = new WeakMap<Element, Item>();
	const seen = new WeakSet<Element>();
	let observer = $state<IntersectionObserver | undefined>();

	// Saved items and search results are browsed, not read through: they are
	// not marked read on scroll (as in Fever), and have no Unread/All or
	// "mark all as read".
	const browsing = scope.kind === 'saved' || scope.kind === 'search';
	// A boolean, so the observer is not rebuilt each time the counts reload.
	const autoRead = $derived(subs.defaults.autoRead && !browsing);
	$effect(() => {
		if (!autoRead) return;
		void phone.current; // the top bar only exists on phones
		const top = document.querySelector('.topbar')?.getBoundingClientRect().height ?? 0;
		const io = new IntersectionObserver(
			(entries) => {
				for (const e of entries) {
					if (!e.rootBounds) continue;
					const above = !e.isIntersecting && e.boundingClientRect.bottom <= e.rootBounds.top + 1;
					if (!above) {
						seen.add(e.target);
						continue;
					}
					const item = itemOf.get(e.target);
					if (item && !item.read && seen.has(e.target) && !keepUnread.has(item.id)) {
						itemState.markRead(item);
					}
				}
			},
			{ rootMargin: `-${Math.round(top)}px 0px 0px 0px` }
		);
		observer = io;
		return () => io.disconnect();
	});

	// A rebuilt list (after "Unread"/"All" or "Mark all as read") starts at
	// the top. Navigations keep SvelteKit's own scroll handling, which runs
	// after this.
	onMount(() => window.scrollTo(0, 0));

	function watchReading(item: Item) {
		return (node: HTMLElement) => {
			if (!observer) return;
			itemOf.set(node, item);
			observer.observe(node);
			return () => observer?.unobserve(node);
		};
	}

	// Load the next page well before the end is reached. An observer only
	// reports changes: if a short page leaves the marker in range, observing
	// it again makes it report anew, so loading goes on.
	let recheck: (() => void) | undefined;
	function loadNear(node: HTMLElement) {
		const again = () => {
			io.unobserve(node);
			io.observe(node);
		};
		const io = new IntersectionObserver(
			async (entries) => {
				if (entries[0]?.isIntersecting && (await loadMore()) && !loadError) again();
			},
			{ rootMargin: '0px 0px 1500px 0px' }
		);
		io.observe(node);
		recheck = again;
		return () => {
			io.disconnect();
			recheck = undefined;
		};
	}

	function retry() {
		loadError = '';
		recheck?.();
	}

	function toggleRead(item: Item) {
		if (item.read) {
			keepUnread.add(item.id);
			itemState.markUnread(item);
		} else {
			itemState.markRead(item);
		}
	}

	/** This list, or with everything every item, up to what had arrived when it was shown. */
	async function markAllRead(everything = false) {
		if (browsing && !everything) return;
		const count = everything ? shown().totalUnread : unread;
		if (count === 0) return;
		const label = count === 1 ? '1 item' : `${count} items`;
		if (
			!confirm(everything ? `Mark all ${label} as read, in every feed?` : `Mark ${label} as read?`)
		) {
			return;
		}
		const target =
			!everything && (scope.kind === 'group' || scope.kind === 'feed')
				? { scope: scope.kind, id: scope.id }
				: { scope: 'all', id: null };
		try {
			// Changes made here that are not sent yet go first. Sent after
			// this, they would undo part of it: an item marked unread a
			// moment ago would stay unread.
			await itemState.flush();
			await api('/api/items/read-all', {
				method: 'POST',
				body: JSON.stringify({ ...target, before: shownAt })
			});
		} catch (err) {
			if (err instanceof ApiError && err.status === 401) {
				void toLogin();
				return;
			}
			alert(`Could not mark the items as read: ${err instanceof Error ? err.message : err}`);
			return;
		}
		await Promise.all([itemState.reloadCountsQuietly(), invalidate('app:stream')]);
	}

	function showRead(show: boolean) {
		if (show === !options.unreadOnly) return;
		setShowReadPref(show);
		invalidate('app:stream');
	}

	// Keyboard shortcuts (see shortcuts.svelte.ts). The current item is the
	// first one still in the reading area, which starts below the phone's
	// top bar: J and K move to the item after or before it, putting its top
	// at that line.
	let list: HTMLElement | undefined = $state();
	const GAP = 12;
	const readingTop = () => document.querySelector('.topbar')?.getBoundingClientRect().height ?? 0;
	const nodes = () => (list ? [...list.querySelectorAll<HTMLElement>(':scope > [data-id]')] : []);
	const itemFor = (node: HTMLElement | undefined) =>
		node && items.find((it) => it.id === Number(node.dataset.id));
	function currentNode() {
		const top = readingTop();
		return nodes().find((n) => n.getBoundingClientRect().bottom > top + 40);
	}
	function scrollToNode(node: Element) {
		window.scrollTo({ top: scrollY + node.getBoundingClientRect().top - readingTop() - GAP });
	}
	// The item after the current one, and the one to go back to: the
	// current one if it is partly scrolled past, else the one before.
	function after() {
		const all = nodes();
		const current = currentNode();
		return current ? all[all.indexOf(current) + 1] : undefined;
	}
	function before() {
		const all = nodes();
		const current = currentNode();
		if (!current) return all.at(-1);
		if (current.getBoundingClientRect().top < readingTop() + GAP - 1) return current;
		return all[all.indexOf(current) - 1];
	}
	// Past the last item: to the end, so it scrolls past the top (and is
	// read). With more to come, the page cannot scroll "Loading…" up there:
	// the last item stays in view, and J goes on into the next page.
	function toEnd() {
		const end = document.querySelector('.end, .more');
		if (end) scrollToNode(end);
	}

	let currentId = $state<number | null>(null);
	const updateCurrent = () => {
		currentId = itemFor(currentNode())?.id ?? null;
	};
	// While keys are used, the current item is marked, and follows the scroll.
	$effect(() => {
		if (!shortcuts.keyboard) return;
		let frame = 0;
		const follow = () => {
			cancelAnimationFrame(frame);
			frame = requestAnimationFrame(updateCurrent);
		};
		updateCurrent();
		addEventListener('scroll', follow, { passive: true });
		return () => {
			cancelAnimationFrame(frame);
			removeEventListener('scroll', follow);
		};
	});

	// The site of an item's feed: its stored address, else the home page of
	// the item's or the feed's address. Web addresses only.
	function siteOf(item: Item): string | null {
		const feed = feedsById.get(item.feedId);
		for (const candidate of [feed?.siteUrl, item.url, feed?.url]) {
			if (!candidate) continue;
			let url: URL;
			try {
				url = new URL(candidate);
			} catch {
				continue; // not an address
			}
			if (url.protocol !== 'http:' && url.protocol !== 'https:') continue;
			return candidate === feed?.siteUrl ? url.href : url.origin + '/';
		}
		return null;
	}

	// Whether an item of this feed belongs in this list.
	function inList(feedId: number): boolean {
		switch (scope.kind) {
			case 'all':
				return true;
			case 'feed':
				return scope.id === feedId;
			case 'group':
				return !!subs.groups.find((g) => g.id === scope.id)?.feedIds.includes(feedId);
			default:
				return false; // saved and search lists show read items anyway
		}
	}

	async function markFeedRead(feedId: number) {
		try {
			await itemState.flush();
			await api('/api/items/read-all', {
				method: 'POST',
				body: JSON.stringify({ scope: 'feed', id: feedId, before: shownAt })
			});
		} catch (err) {
			if (err instanceof ApiError && err.status === 401) {
				void toLogin();
				return;
			}
			alert(`Could not mark the feed as read: ${err instanceof Error ? err.message : err}`);
			return;
		}
		for (const it of items) if (it.feedId === feedId) it.read = true;
		await itemState.reloadCountsQuietly();
	}

	onMount(() =>
		shortcuts.setList({
			next() {
				const node = after();
				if (node) scrollToNode(node);
				else toEnd();
			},
			previous() {
				const node = before();
				if (node) scrollToNode(node);
				else window.scrollTo({ top: 0 });
			},
			page(back) {
				const top = readingTop();
				const height = innerHeight - top - 40;
				const node = back ? before() : after();
				const y = node?.getBoundingClientRect().top;
				const inView = y !== undefined && (back ? y > top - height : y < innerHeight - 40);
				if (node && inView) scrollToNode(node);
				else window.scrollBy({ top: back ? -height : height });
			},
			open() {
				const item = itemFor(currentNode());
				if (!item?.url) return;
				window.open(item.url, '_blank', 'noopener,noreferrer');
				itemState.markRead(item);
			},
			toggleSaved() {
				const item = itemFor(currentNode());
				if (!item) return;
				itemState.setSaved(item, !item.saved).catch((err) => {
					alert(`Could not change the saved state: ${err instanceof Error ? err.message : err}`);
				});
			},
			visitSite(markRead) {
				const item = itemFor(currentNode());
				const site = item && siteOf(item);
				if (!item || !site) return;
				window.open(site, '_blank', 'noopener,noreferrer');
				if (markRead) void markFeedRead(item.feedId);
			},
			markAllRead(everything) {
				void markAllRead(everything);
			},
			toggleShowRead() {
				if (!browsing) showRead(options.unreadOnly);
			},
			unreadAgain(restored) {
				const loaded = new Map(items.map((it) => [it.id, it]));
				let missing = false;
				for (const { id, feedId } of restored) {
					const it = loaded.get(id);
					if (it) it.read = false;
					else missing ||= inList(feedId);
				}
				// Gone from a list of unread items (after "mark all as
				// read"): it is loaded again to show them, as Fever did.
				if (missing && options.unreadOnly) void invalidate('app:stream');
			}
		})
	);

	// Reads and saves made on other devices, applied when the app comes back
	// to the foreground and every few minutes. Changes come in pages.
	let syncedAt = initial.page.loadedAt;
	async function syncChanges() {
		try {
			const byId = new Map(items.map((it) => [it.id, it]));
			let query = `since=${encodeURIComponent(syncedAt)}`;
			let startedAt: string | undefined;
			for (let pages = 0; pages < 100; pages++) {
				const res = await api<ItemChanges>(`/api/items/changes?${query}`);
				startedAt ??= res.loadedAt;
				for (const c of res.changes) {
					const it = byId.get(c.id);
					if (it && !itemState.isPending(c.id)) {
						it.read = c.read;
						it.saved = c.saved;
					}
				}
				if (!res.next) break;
				query = `after=${encodeURIComponent(res.next)}`;
			}
			// Next time, from when this sync started.
			if (startedAt) syncedAt = startedAt;
		} catch {
			// offline: tried again on the next resume
		}
	}
	$effect(() => onResume(() => void syncChanges()));
</script>

{#if !browsing}
	<div class="toolbar">
		<div class="segmented" role="group" aria-label="Items to show">
			<button aria-pressed={options.unreadOnly} onclick={() => showRead(false)}>Unread</button>
			<button aria-pressed={!options.unreadOnly} onclick={() => showRead(true)}>All</button>
		</div>
		<button class="mark-all" onclick={() => markAllRead()} disabled={unread === 0}>
			Mark all as read
		</button>
	</div>
{/if}

{#if items.length === 0}
	<div class="empty">
		{#if scope.kind === 'saved'}
			<p>No saved items. Save an item with its bookmark button to keep it here.</p>
		{:else if scope.kind === 'search'}
			<p>Nothing found for “{scope.q}”.</p>
		{:else if options.unreadOnly}
			<p>No unread items.</p>
		{:else}
			<p>No items.</p>
		{/if}
	</div>
{:else}
	<div class="items" bind:this={list}>
		{#each items as item (item.id)}
			<div data-id={item.id} {@attach watchReading(item)}>
				<ItemView
					{item}
					feed={feedsById.get(item.feedId)}
					feedHref={feedHref(item.feedId)}
					showImages={options.showImages}
					now={clock.now}
					current={shortcuts.keyboard && item.id === currentId}
					onToggleRead={toggleRead}
				/>
			</div>
		{/each}
	</div>

	{#if next}
		<div class="more" {@attach loadNear}>
			{#if loadError}
				<p>{loadError}</p>
				<button onclick={retry}>Try again</button>
			{:else}
				<p>Loading…</p>
			{/if}
		</div>
	{:else}
		<!-- Room to scroll the last items past the top, so they get marked read too. -->
		<div class="end" class:room={autoRead}>
			<p>
				{scope.kind === 'saved'
					? 'No more saved items.'
					: scope.kind === 'search'
						? 'No more results.'
						: 'You have reached the end.'}
			</p>
		</div>
	{/if}
{/if}

<style>
	.toolbar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		margin-bottom: 12px;
	}

	.segmented {
		display: inline-flex;
		border-radius: var(--radius);
		box-shadow: var(--shadow);
		overflow: hidden;
	}

	.segmented button,
	.mark-all,
	.more button {
		height: 30px;
		padding: 0 12px;
		border: 0;
		background: var(--row);
		color: var(--text-muted);
		font-size: 13px;
		cursor: pointer;
	}

	.segmented button + button {
		border-left: 1px solid var(--row-line);
	}

	.segmented button[aria-pressed='true'] {
		background: var(--row-selected);
		color: var(--row-selected-text);
	}

	.mark-all,
	.more button {
		border-radius: var(--radius);
		box-shadow: var(--shadow);
	}

	.mark-all:hover:not(:disabled),
	.segmented button:hover:not([aria-pressed='true']) {
		background: var(--row-hover);
		color: var(--text);
	}

	.mark-all:disabled {
		opacity: 0.5;
		cursor: default;
	}

	@media (pointer: coarse) {
		.segmented button,
		.mark-all {
			height: 40px;
		}
	}

	.empty,
	.more,
	.end {
		padding: 24px;
		color: var(--text-muted);
		text-align: center;
	}

	.empty {
		border-radius: var(--radius);
		background: var(--surface);
		box-shadow: var(--shadow);
	}

	.empty p,
	.more p,
	.end p {
		margin: 0 0 8px;
	}

	/* A full screen of room: even the last item can scroll above the top. */
	.end.room {
		min-height: 100dvh;
	}
</style>
