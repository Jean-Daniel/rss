<script lang="ts">
	import { afterNavigate, goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api } from '#lib/api/client.ts';
	import FeedList from '#lib/components/FeedList.svelte';
	import { clock, onResume } from '#lib/foreground.svelte.ts';
	import { itemState, withLocalCounts } from '#lib/item-state.svelte.ts';
	import SourceList from '#lib/components/SourceList.svelte';
	import { contextGroup, selectionFrom, selectionTitle, unreadOf } from '#lib/selection.ts';
	import { ago } from '#lib/time.ts';
	import { phone, ui, wide } from '#lib/ui.svelte.ts';
	import type { LayoutProps } from './$types';

	let { data, children }: LayoutProps = $props();

	// Counts include reads made here that the server has not reported back yet.
	const subs = $derived(withLocalCounts(data.subscriptions, itemState.counts));
	const selection = $derived(
		selectionFrom(page.route.id, page.params, page.url.searchParams, subs)
	);
	const group = $derived(contextGroup(selection));
	const feeds = $derived(
		group ? subs.feeds.filter((f) => group.feedIds.includes(f.id)) : subs.feeds
	);
	const title = $derived(selectionTitle(selection));
	const unread = $derived(unreadOf(selection, subs));

	const failing = $derived(subs.feeds.filter((f) => f.error !== null).length);

	afterNavigate(() => ui.closePanels());

	// Refreshing: POST starts it on the server; the subscriptions are then
	// reloaded every 2 seconds until the server says it is done.
	let starting = $state(false);
	let refreshError = $state('');
	const refreshing = $derived(starting || subs.refreshing);

	async function refreshAll() {
		if (refreshing) return;
		starting = true;
		refreshError = '';
		try {
			await api('/api/refresh', { method: 'POST' });
			await itemState.reloadCounts();
		} catch (err) {
			refreshError = err instanceof Error ? err.message : String(err);
		} finally {
			starting = false;
		}
	}

	$effect(() => {
		if (!subs.refreshing) return;
		const timer = setTimeout(() => itemState.reloadCounts(), 2000);
		return () => clearTimeout(timer);
	});

	// Keep counts current, including reads made on another device.
	$effect(() => onResume(() => itemState.reloadCounts()));

	async function logout() {
		try {
			await api('/api/session', { method: 'DELETE' });
		} catch (err) {
			alert(`Could not log out: ${err instanceof Error ? err.message : err}`);
			return;
		}
		await goto('/login', { refreshAll: true });
	}

	function onkeydown(event: KeyboardEvent) {
		if (event.key === 'Escape') ui.closePanels();
	}
</script>

<svelte:head>
	<title>{unread > 0 ? `(${unread}) ` : ''}{title} · Fever</title>
</svelte:head>

<svelte:window {onkeydown} />

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
		<h1 class="topbar-title">{title}</h1>
		{#if unread > 0}<span class="topbar-count">{unread}</span>{/if}
		{@render refreshButton()}
	</header>

	<button class="backdrop" tabindex="-1" aria-label="Close" onclick={() => ui.closePanels()}
	></button>

	<div class="panels" inert={phone.current && !ui.drawerOpen}>
		<div class="sidebar">
			<div class="brand">
				<a href="/" class="logo" aria-label="Fever, all items">fever<span>°</span></a>
				<button class="icon-button" onclick={logout} title="Log out" aria-label="Log out">
					<svg viewBox="0 0 18 18" aria-hidden="true">
						<path d="M7 3H3v12h4M12 5l4 4-4 4M16 9H7" />
					</svg>
				</button>
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
		</div>

		<div class="feeds">
			<FeedList {feeds} {group} {selection} retentionWeeks={subs.retentionWeeks} />
		</div>
	</div>

	<main class="content">
		<header class="content-header">
			<h1>{title}</h1>
			{#if unread > 0}<span class="unread">{unread} unread</span>{/if}
			<button
				class="icon-button feeds-toggle"
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
	.shell {
		display: grid;
		grid-template-columns: var(--sidebar-w) minmax(0, 1fr);
		gap: var(--gap);
		align-items: start;
		max-width: 1180px;
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
		color: #a26731;
	}

	.status-error {
		margin: 0 4px;
		font-size: 12px;
		color: var(--accent);
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

	.feeds-toggle {
		display: inline-flex;
		gap: 6px;
		margin-left: auto;
	}

	.feeds-toggle[aria-pressed='true'] {
		color: var(--text);
	}

	.backdrop {
		position: fixed;
		inset: 0;
		padding: 0;
		border: 0;
		background: rgb(0 0 0 / 0.3);
	}

	/* Wide: sections | feeds (optional) | items */
	@media (min-width: 1100px) {
		.shell.feeds-visible {
			grid-template-columns: var(--sidebar-w) var(--feeds-w) minmax(0, 1fr);
			max-width: 1440px;
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
			padding: 12px;
			border-radius: var(--radius);
			background: var(--page);
			box-shadow: 0 10px 40px rgb(0 0 0 / 0.3);
		}

		.shell.feeds-overlay .backdrop {
			display: block;
			z-index: 10;
			background: rgb(0 0 0 / 0.08);
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
			background: rgb(236 236 236 / 0.92);
			backdrop-filter: blur(10px);
			-webkit-backdrop-filter: blur(10px);
			border-bottom: 1px solid var(--row-line);
		}

		.topbar-title {
			flex: 1;
			min-width: 0;
			margin: 0;
			font-size: 17px;
			overflow: hidden;
			text-overflow: ellipsis;
			white-space: nowrap;
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
		}

		.drawer-open .panels {
			transform: none;
			box-shadow: 4px 0 24px rgb(0 0 0 / 0.25);
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
