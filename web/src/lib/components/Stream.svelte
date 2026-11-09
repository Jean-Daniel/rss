<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { invalidate } from '$app/navigation';
	import { page } from '$app/state';
	import { api } from '#lib/api/client.ts';
	import type { Item, ItemChanges, ItemPage, Subscriptions } from '#lib/api/types.ts';
	import { clock, onResume } from '#lib/foreground.svelte.ts';
	import { itemState, withLocalCounts } from '#lib/item-state.svelte.ts';
	import { setShowReadPref } from '#lib/prefs.ts';
	import { itemsURL, type StreamData } from '#lib/stream.ts';
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

	const counted = $derived(withLocalCounts(subs, itemState.counts));
	const feedsById = $derived(new Map(counted.feeds.map((f) => [f.id, f])));
	const unread = $derived.by(() => {
		switch (scope.kind) {
			case 'all':
				return counted.totalUnread;
			case 'group':
				return counted.groups.find((g) => g.id === scope.id)?.unread ?? 0;
			case 'feed':
				return feedsById.get(scope.id)?.unread ?? 0;
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

	// A boolean, so the observer is not rebuilt each time the counts reload.
	// Never in the Saved list: browsing saved items does not read them, as
	// in Fever.
	const autoRead = $derived(subs.defaults.autoRead && scope.kind !== 'saved');
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

	async function markAllRead() {
		if (scope.kind === 'saved') return;
		const label = unread === 1 ? '1 item' : `${unread} items`;
		if (!confirm(`Mark ${label} as read?`)) return;
		try {
			await api('/api/items/read-all', {
				method: 'POST',
				body: JSON.stringify({
					scope: scope.kind,
					id: scope.kind === 'all' ? null : scope.id,
					before: shownAt
				})
			});
			await Promise.all([itemState.reloadCounts(), invalidate('app:stream')]);
		} catch (err) {
			alert(`Could not mark the items as read: ${err instanceof Error ? err.message : err}`);
		}
	}

	function showRead(show: boolean) {
		if (show === !options.unreadOnly) return;
		setShowReadPref(show);
		invalidate('app:stream');
	}

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

{#if scope.kind !== 'saved'}
	<div class="toolbar">
		<div class="segmented" role="group" aria-label="Items to show">
			<button aria-pressed={options.unreadOnly} onclick={() => showRead(false)}>Unread</button>
			<button aria-pressed={!options.unreadOnly} onclick={() => showRead(true)}>All</button>
		</div>
		<button class="mark-all" onclick={markAllRead} disabled={unread === 0}>
			Mark all as read
		</button>
	</div>
{/if}

{#if items.length === 0}
	<div class="empty">
		{#if scope.kind === 'saved'}
			<p>No saved items. Save an item with its bookmark button to keep it here.</p>
		{:else if options.unreadOnly}
			<p>No unread items.</p>
		{:else}
			<p>No items.</p>
		{/if}
	</div>
{:else}
	<div class="items">
		{#each items as item (item.id)}
			<div {@attach watchReading(item)}>
				<ItemView
					{item}
					feed={feedsById.get(item.feedId)}
					feedHref={feedHref(item.feedId)}
					showImages={options.showImages}
					now={clock.now}
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
		<div class="end" class:room={autoRead && scope.kind !== 'saved'}>
			<p>{scope.kind === 'saved' ? 'No more saved items.' : 'You have reached the end.'}</p>
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
