<script lang="ts">
	import { afterNavigate, goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api } from '#lib/api/client.ts';
	import type { RefreshRequest, UnreadRecent } from '#lib/api/types.ts';
	import ShortcutsHelp from '#lib/components/ShortcutsHelp.svelte';
	import { failure } from '#lib/manage.ts';
	import { handleKey, shortcuts, type AppKeys } from '#lib/shortcuts.svelte.ts';
	import FeedList from '#lib/components/FeedList.svelte';
	import { clock, onResume } from '#lib/foreground.svelte.ts';
	import { install } from '#lib/install.svelte.ts';
	import { itemState, withLocalCounts } from '#lib/item-state.svelte.ts';
	import SourceList from '#lib/components/SourceList.svelte';
	import {
		contextGroup,
		editHref,
		selectionFrom,
		selectionTitle,
		unreadOf
	} from '#lib/selection.ts';
	import { setShownSubscriptions } from '#lib/subscriptions.ts';
	import { swipeToClose } from '#lib/swipe.ts';
	import { ago } from '#lib/time.ts';
	import { phone, ui, wide } from '#lib/ui.svelte.ts';
	import type { LayoutProps } from './$types';

	let { data, children }: LayoutProps = $props();

	// Counts include reads made here that the server has not reported back yet.
	const subs = $derived(withLocalCounts(data.subscriptions, itemState.counts));
	setShownSubscriptions(() => subs);
	const selection = $derived(
		selectionFrom(page.route.id, page.params, page.url.searchParams, subs)
	);
	const group = $derived(contextGroup(selection));
	const feeds = $derived(
		group ? subs.feeds.filter((f) => group.feedIds.includes(f.id)) : subs.feeds
	);
	const title = $derived(selectionTitle(selection));
	const edit = $derived(editHref(selection));
	const unread = $derived(unreadOf(selection, subs));

	const failing = $derived(subs.feeds.filter((f) => f.error !== null).length);

	afterNavigate(() => ui.closePanels());

	// Refreshing: POST starts it on the server; the subscriptions are then
	// reloaded every 2 seconds until the server says it is done.
	let starting = $state(false);
	let refreshError = $state('');
	const refreshing = $derived(starting || subs.refreshing);

	/** Every feed, or only a group's or a feed's. */
	async function refresh(only?: RefreshRequest) {
		if (refreshing) return;
		starting = true;
		refreshError = '';
		try {
			await api('/api/refresh', {
				method: 'POST',
				body: only ? JSON.stringify(only) : undefined
			});
			await itemState.reloadCounts();
		} catch (err) {
			refreshError = err instanceof Error ? err.message : String(err);
		} finally {
			starting = false;
		}
	}

	// A boolean: reading items changes `subs` all the time, and that must not
	// restart the wait below.
	const serverRefreshing = $derived(data.subscriptions.refreshing);
	$effect(() => {
		if (!serverRefreshing) return;
		let timer: ReturnType<typeof setTimeout>;
		let stopped = false;
		const poll = async () => {
			await itemState.reloadCountsQuietly();
			if (!stopped) timer = setTimeout(poll, 2000);
		};
		timer = setTimeout(poll, 2000);
		return () => {
			stopped = true;
			clearTimeout(timer);
		};
	});

	// Keep counts current, including reads made on another device.
	$effect(() => onResume(() => void itemState.reloadCountsQuietly()));

	async function logout() {
		try {
			await api('/api/session', { method: 'DELETE' });
		} catch (err) {
			alert(`Could not log out: ${err instanceof Error ? err.message : err}`);
			return;
		}
		await goto('/login', { refreshAll: true });
	}

	const refreshAll = () => refresh();

	// Keyboard shortcuts: the list on screen handles those that act on items.
	const app: AppKeys = {
		go: (path) => void goto(path),
		// Shift+R: the feed or group shown, in its list or its settings.
		refresh(shown) {
			const feed = selection.kind === 'feed' || selection.kind === 'manage' ? selection.feed : null;
			const group =
				selection.kind === 'group' || selection.kind === 'manage' ? selection.group : null;
			if (shown && feed) void refresh({ feedId: feed.id, groupId: null });
			else if (shown && group) void refresh({ feedId: null, groupId: group.id });
			else void refresh();
		},
		toggleFeeds: () => ui.toggleFeeds(),
		edit() {
			if (edit) void goto(edit);
		},
		search() {
			const field = document.querySelector<HTMLInputElement>('input[name="q"]');
			if (!field) {
				void goto('/search');
				return;
			}
			field.focus();
			field.select();
		},
		undoReads: () => void undoReads(),
		closePanels() {
			const open = ui.drawerOpen || ui.feedsOverlay;
			ui.closePanels();
			return open;
		}
	};

	// Z: the items read last, on any device, are unread again.
	async function undoReads() {
		try {
			await itemState.flush(); // reads not sent yet count too
			const res = await api<UnreadRecent>('/api/items/unread-recent', { method: 'POST' });
			shortcuts.list?.unreadAgain(res.items);
			await itemState.reloadCountsQuietly();
		} catch (err) {
			alert(`Could not mark the items as unread: ${failure(err)}`);
		}
	}

	function onkeydown(event: KeyboardEvent) {
		if (handleKey(event, app)) event.preventDefault();
	}

	// The phone's title bar, tapped: back to the first item.
	function scrollToTop() {
		const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
		window.scrollTo({ top: 0, behavior: reduce ? 'instant' : 'smooth' });
	}
</script>

<svelte:head>
	<title>{unread > 0 ? `(${unread}) ` : ''}{title} · Fever</title>
</svelte:head>

<svelte:window {onkeydown} />

<ShortcutsHelp />

{#snippet editButton()}
	{#if edit}
		{@const what = selection.kind === 'group' ? 'group' : 'feed'}
		<a class="icon-button" href={edit} title="Edit this {what}" aria-label="Edit this {what}">
			<svg viewBox="0 0 18 18" aria-hidden="true">
				<path d="M11.5 3.5l3 3L6 15H3v-3zM9.5 5.5l3 3" />
			</svg>
		</a>
	{/if}
{/snippet}

{#snippet refreshButton()}
	<button
		class="icon-button refresh"
		class:spinning={refreshing}
		onclick={refreshAll}
		disabled={refreshing}
		title="Refresh all feeds"
		aria-label="Refresh all feeds"
	>
		<svg viewBox="0 0 18 18" aria-hidden="true">
			<path d="M15 9a6 6 0 1 1-1.76-4.24M15 2.5v3.5h-3.5" />
		</svg>
	</button>
{/snippet}

<div
	class="shell"
	class:feeds-visible={ui.feedsVisible}
	class:feeds-overlay={ui.feedsOverlay}
	class:drawer-open={ui.drawerOpen}
>
	<header class="topbar">
		<button
			class="icon-button"
			onclick={() => (ui.drawerOpen = true)}
			aria-label="Groups and feeds"
			aria-expanded={ui.drawerOpen}
		>
			<svg viewBox="0 0 18 18" aria-hidden="true"><path d="M2 4h14M2 9h14M2 14h14" /></svg>
		</button>
		<h1 class="topbar-title">
			<button onclick={scrollToTop} title="Back to the top">{title}</button>
		</h1>
		{#if unread > 0}<span class="topbar-count">{unread}</span>{/if}
		{@render editButton()}
		{@render refreshButton()}
	</header>

	<button class="backdrop" tabindex="-1" aria-label="Close" onclick={() => ui.closePanels()}
	></button>

	<div
		class="panels"
		inert={phone.current && !ui.drawerOpen}
		{@attach phone.current && swipeToClose(() => (ui.drawerOpen = false))}
	>
		<div class="sidebar">
			<div class="brand">
				<a href="/" class="logo" aria-label="Fever, all items">fever<span>°</span></a>
				<span class="brand-actions">
					<a
						class="icon-button"
						href="/settings"
						title="Settings: feeds, groups, preferences"
						aria-label="Settings"
						aria-current={page.route.id === '/(reader)/settings' ? 'page' : undefined}
					>
						<svg viewBox="0 0 18 18" aria-hidden="true">
							<path d="M3 5h5M12 5h3M3 13h3M10 13h5" />
							<circle cx="10" cy="5" r="2" />
							<circle cx="8" cy="13" r="2" />
						</svg>
					</a>
					<button class="icon-button" onclick={logout} title="Log out" aria-label="Log out">
						<svg viewBox="0 0 18 18" aria-hidden="true">
							<path d="M7 3H3v12h4M12 5l4 4-4 4M16 9H7" />
						</svg>
					</button>
				</span>
			</div>
			<SourceList {subs} {selection} />
			<div class="status">
				{@render refreshButton()}
				<p>
					{subs.feeds.length}
					{subs.feeds.length === 1 ? 'feed' : 'feeds'} ·
					{#if refreshing}
						refreshing…
					{:else if subs.lastRefreshedAt}
						refreshed {ago(subs.lastRefreshedAt, clock.now)}
					{:else}
						never refreshed
					{/if}
					{#if failing > 0}<br /><span class="failing">{failing} with problems</span>{/if}
				</p>
			</div>
			{#if refreshError}<p class="status-error" role="alert">{refreshError}</p>{/if}
			{#if install.available}
				<button class="icon-button labeled install" onclick={() => install.prompt()}>
					<svg viewBox="0 0 18 18" aria-hidden="true">
						<path d="M9 2.5v9M5 8l4 4 4-4M3 15.5h12" />
					</svg>
					<span>Install app</span>
				</button>
			{/if}
		</div>

		<div class="feeds">
			<FeedList {feeds} {group} {selection} retentionWeeks={subs.retentionWeeks} />
		</div>
	</div>

	<main class="content">
		<header class="content-header">
			<h1>{title}</h1>
			{#if unread > 0}<span class="unread">{unread} unread</span>{/if}
			<span class="header-actions">
				{@render editButton()}
			</span>
			<button
				class="icon-button labeled feeds-toggle"
				onclick={() => ui.toggleFeeds()}
				aria-pressed={wide.current ? ui.feedsVisible : ui.feedsOverlay}
				title="Show or hide the feed list"
			>
				<svg viewBox="0 0 18 18" aria-hidden="true">
					<rect x="2" y="3" width="14" height="12" rx="1.5" /><path d="M7 3v12" />
				</svg>
				<span>Feeds</span>
			</button>
		</header>
		{@render children()}
	</main>
</div>

<style>
	/* The lists, then the reading column at its width, centered. */
	.shell {
		display: grid;
		grid-template-columns: var(--sidebar-w) minmax(0, 1fr);
		gap: var(--gap);
		align-items: start;
		max-width: calc(var(--sidebar-w) + var(--gap) + var(--stream-w) + 32px);
		margin: 0 auto;
		padding: 16px max(16px, env(safe-area-inset-right)) 48px max(16px, env(safe-area-inset-left));
	}

	.panels {
		display: contents;
	}

	.sidebar,
	.feeds {
		position: sticky;
		top: 16px;
		max-height: calc(100dvh - 32px);
		overflow-y: auto;
		overscroll-behavior: contain;
		scrollbar-width: thin;
	}

	.feeds,
	.topbar,
	.backdrop {
		display: none;
	}

	.brand {
		display: flex;
		align-items: center;
		justify-content: space-between;
		margin-bottom: 12px;
		padding-left: 4px;
	}

	.logo {
		font-size: 21px;
		letter-spacing: -0.02em;
		color: var(--text);
	}

	.logo span {
		color: var(--accent-strong);
	}

	.status {
		display: flex;
		align-items: center;
		gap: 4px;
		margin: 10px 0;
		font-size: 12px;
		color: var(--text-faint);
	}

	.status p {
		margin: 0;
	}

	.failing {
		color: var(--warn);
	}

	.status-error {
		margin: 0 4px;
		font-size: 12px;
		color: var(--accent);
	}

	.install {
		font-size: 12px;
	}

	.refresh:disabled {
		cursor: default;
	}

	.refresh.spinning svg {
		animation: spin 1s linear infinite;
	}

	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}

	.icon-button svg {
		fill: none;
		stroke: currentColor;
		stroke-width: 1.6;
		stroke-linecap: round;
		stroke-linejoin: round;
	}

	.content {
		min-width: 0;
	}

	.content-header {
		display: flex;
		align-items: center;
		gap: 12px;
		min-height: 36px;
		margin-bottom: 12px;
	}

	.content-header h1 {
		margin: 0;
		font-size: 19px;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.unread {
		color: var(--text-faint);
		white-space: nowrap;
	}

	.header-actions {
		margin-left: auto;
	}

	.brand-actions {
		display: flex;
	}

	.brand-actions [aria-current='page'] {
		background: var(--row-hover);
		color: var(--text);
	}

	.feeds-toggle[aria-pressed='true'] {
		color: var(--text);
	}

	.backdrop {
		position: fixed;
		inset: 0;
		padding: 0;
		border: 0;
		background: var(--backdrop);
	}

	/* Wide: sections | feeds (optional) | items */
	@media (min-width: 1100px) {
		.shell.feeds-visible {
			grid-template-columns: var(--sidebar-w) var(--feeds-w) minmax(0, 1fr);
			max-width: calc(var(--sidebar-w) + var(--feeds-w) + 2 * var(--gap) + var(--stream-w) + 32px);
		}

		.shell.feeds-visible .feeds {
			display: block;
		}
	}

	/* Medium: sections | items, with the feed list sliding over the items. */
	@media (min-width: 700px) and (max-width: 1099px) {
		.shell.feeds-overlay .feeds {
			display: block;
			position: fixed;
			top: 16px;
			bottom: 16px;
			left: calc(max(16px, env(safe-area-inset-left)) + var(--sidebar-w) + var(--gap));
			z-index: 20;
			width: var(--feeds-w);
			max-height: none;
			padding: 12px 8px;
			border-radius: var(--radius-card);
			background: var(--page);
			box-shadow: var(--shadow-float);
		}

		.shell.feeds-overlay .backdrop {
			display: block;
			z-index: 10;
			background: color-mix(in srgb, var(--backdrop) 30%, transparent);
		}
	}

	/* Phone: one column, sections and feeds in a drawer. */
	@media (max-width: 699px) {
		.shell {
			display: block;
			padding: 0 0 48px;
		}

		.topbar {
			display: flex;
			align-items: center;
			gap: 4px;
			position: sticky;
			top: 0;
			z-index: 5;
			padding: env(safe-area-inset-top) max(8px, env(safe-area-inset-right)) 0
				max(4px, env(safe-area-inset-left));
			min-height: calc(48px + env(safe-area-inset-top));
			background: color-mix(in srgb, var(--page) 88%, transparent);
			backdrop-filter: blur(12px);
			-webkit-backdrop-filter: blur(12px);
			border-bottom: 1px solid var(--line);
		}

		.topbar-title {
			flex: 1;
			min-width: 0;
			margin: 0;
			font-size: 17px;
		}

		.topbar-title button {
			display: block;
			width: 100%;
			min-height: 44px;
			padding: 0 4px;
			border: 0;
			background: none;
			font-weight: inherit;
			text-align: left;
			overflow: hidden;
			text-overflow: ellipsis;
			white-space: nowrap;
			cursor: pointer;
		}

		.topbar-count {
			color: var(--text-faint);
			font-variant-numeric: tabular-nums;
		}

		.content-header {
			display: none;
		}

		.content {
			padding: 12px max(12px, env(safe-area-inset-right)) 0 max(12px, env(safe-area-inset-left));
		}

		.panels {
			display: block;
			position: fixed;
			inset: 0 auto 0 0;
			z-index: 20;
			width: min(86vw, 340px);
			overflow-y: auto;
			overscroll-behavior: contain;
			padding: calc(12px + env(safe-area-inset-top)) 12px calc(16px + env(safe-area-inset-bottom))
				max(12px, env(safe-area-inset-left));
			background: var(--page);
			transform: translateX(-102%);
			transition: transform 0.2s ease;
			touch-action: pan-y; /* horizontal moves close it (swipeToClose) */
		}

		.drawer-open .panels {
			transform: none;
			box-shadow: var(--shadow-float);
		}

		.sidebar,
		.feeds {
			position: static;
			max-height: none;
			overflow: visible;
		}

		.feeds {
			display: block;
			margin-top: 20px;
		}

		.drawer-open .backdrop {
			display: block;
			z-index: 15;
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.panels {
			transition: none;
		}

		.refresh.spinning svg {
			animation: none;
			opacity: 0.5;
		}
	}
</style>
