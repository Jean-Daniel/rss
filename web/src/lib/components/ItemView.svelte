<script lang="ts">
	import type { Feed, Item } from '#lib/api/types.ts';
	import { itemState } from '#lib/item-state.svelte.ts';
	import { when } from '#lib/time.ts';
	import FeedIcon from './FeedIcon.svelte';

	let {
		item,
		feed,
		feedHref,
		showImages,
		now,
		current = false,
		onToggleRead
	}: {
		item: Item;
		feed: Feed | undefined;
		feedHref: string;
		showImages: boolean;
		now: number;
		/** The item the keys act on, chosen with them or the mouse. */
		current?: boolean;
		onToggleRead: (item: Item) => void;
	} = $props();

	let saving = $state(false);

	async function toggleSaved() {
		saving = true;
		try {
			await itemState.setSaved(item, !item.saved);
		} catch (err) {
			alert(`Could not change the saved state: ${err instanceof Error ? err.message : err}`);
		} finally {
			saving = false;
		}
	}
</script>

<article
	class="item"
	class:read={item.read}
	class:saved={item.saved}
	class:current
	aria-labelledby="item-{item.id}"
	aria-current={current ? 'true' : undefined}
	tabindex="-1"
>
	<!-- The source: the site, when, who; and the item's buttons. -->
	<header class="source">
		<div class="meta">
			{#if feed}
				<a class="feed" href={feedHref}>
					<FeedIcon faviconId={feed.faviconId} />
					<span>{feed.title}</span>
				</a>
			{/if}
			<time datetime={item.publishedAt} title={new Date(item.publishedAt).toLocaleString()}
				>{when(item.publishedAt, now)}</time
			>
			{#if item.author}<span class="author">{item.author}</span>{/if}
		</div>
		<div class="actions">
			<button
				class="action save"
				aria-pressed={item.saved}
				disabled={saving}
				title={item.saved ? 'Remove from saved' : 'Save'}
				aria-label={item.saved ? 'Remove from saved' : 'Save'}
				onclick={toggleSaved}
			>
				<svg viewBox="0 0 18 18" aria-hidden="true"><path d="M5 2.5h8v13l-4-3-4 3z" /></svg>
			</button>
			<button
				class="action read-toggle"
				aria-pressed={!item.read}
				title={item.read ? 'Mark as unread' : 'Mark as read'}
				aria-label={item.read ? 'Mark as unread' : 'Mark as read'}
				onclick={() => onToggleRead(item)}
			>
				<svg viewBox="0 0 18 18" aria-hidden="true"><circle cx="9" cy="9" r="4" /></svg>
			</button>
		</div>
	</header>
	<h2 class="title" id="item-{item.id}">
		{#if item.url}
			<!-- Opening the article counts as reading it (also with a middle click). -->
			<a
				href={item.url}
				target="_blank"
				rel="noopener noreferrer"
				onclick={() => itemState.markRead(item)}
				onauxclick={(e) => e.button === 1 && itemState.markRead(item)}>{item.title || 'Untitled'}</a
			>
		{:else}
			{item.title || 'Untitled'}
		{/if}
	</h2>
	<!-- Sanitized on the server (internal/content). -->
	<div class="content" class:no-images={!showImages}>{@html item.content}</div>
</article>

<style>
	/* A card per item, so each one's end is clear. */
	.item {
		margin-bottom: 16px;
		border-radius: var(--radius-card);
		background: var(--surface);
		box-shadow: var(--shadow);
		overflow: hidden;
	}

	/* Focused by J and K (see Stream): .current shows it. */
	.item:focus {
		outline: none;
	}

	.item.current {
		box-shadow:
			0 0 0 2px var(--accent-strong),
			var(--shadow);
	}

	.source {
		display: flex;
		align-items: center;
		gap: 8px;
		min-height: 36px;
		padding: 0px 6px 0px 18px;
		border-bottom: 1px solid var(--line);
		background: var(--unread-tint); /* read items: see below */
	}

	.meta {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: 0 8px;
		overflow: hidden;
		font-size: 12.5px;
		color: var(--text-faint);
		white-space: nowrap;
	}

	.meta > * + *::before {
		content: '·';
		margin-right: 8px;
		color: var(--text-faint);
	}

	.feed {
		display: inline-flex;
		align-items: center;
		gap: 7px;
		min-width: 0;
		color: var(--text-muted);
	}

	/* When short of room, the author gives way first, then the site. */
	.feed {
		flex: 0 1 auto;
	}

	time {
		flex: none;
	}

	.author {
		flex: 0 4 auto;
		min-width: 3em;
	}

	.feed span,
	.author {
		overflow: hidden;
		text-overflow: ellipsis;
	}

	@media (max-width: 420px) {
		.author {
			display: none;
		}
	}

	.actions {
		display: flex;
		flex: none;
	}

	.action {
		display: grid;
		place-items: center;
		width: 34px;
		height: 34px;
		padding: 0;
		border: 0;
		border-radius: var(--radius);
		background: transparent;
		color: var(--text-faint);
		cursor: pointer;
		-webkit-tap-highlight-color: transparent;
	}

	.action:hover,
	.action:active {
		background: var(--row-hover);
		color: var(--text-muted);
	}

	.action svg {
		width: 17px;
		height: 17px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.6;
		stroke-linejoin: round;
	}

	.save[aria-pressed='true'] {
		color: var(--accent-strong);
	}

	.save[aria-pressed='true'] svg {
		fill: currentColor;
	}

	/* Unread: a filled dot, as in Fever's unread marker. */
	.read-toggle[aria-pressed='true'] {
		color: var(--accent-strong);
	}

	.read-toggle[aria-pressed='true'] svg {
		fill: currentColor;
	}

	@media (pointer: coarse) {
		.action {
			width: 44px;
			height: 44px;
		}
	}

	.title {
		margin: 0;
		padding: 16px 24px 0;
		font-family: var(--font-reading);
		font-size: 23px;
		font-weight: 600;
		line-height: 1.25;
		overflow-wrap: anywhere;
	}

	.title a {
		font-weight: inherit;
		color: var(--text);
	}

	.title a:hover {
		color: var(--accent);
	}

	/* Read items step back. */
	.read .source {
		background: var(--surface-tint);
	}

	.read .title,
	.read .title a {
		font-weight: 500;
		color: var(--text-muted);
	}

	.read .content {
		color: var(--text-muted);
	}

	/* Article text. */
	.content {
		padding: 12px 24px 24px;
		font-family: var(--font-reading);
		font-size: 17.5px;
		line-height: 1.62;
		overflow-wrap: break-word;
	}

	.content :global(:first-child) {
		margin-top: 0;
	}

	.content :global(:last-child) {
		margin-bottom: 0;
	}

	.content :global(p) {
		margin: 0 0 0.9em;
	}

	.content :global(a) {
		font-weight: normal;
		color: var(--accent);
		text-decoration: underline;
		text-decoration-color: color-mix(in srgb, var(--accent) 35%, transparent);
		text-underline-offset: 2px;
	}

	.content :global(img),
	.content :global(video) {
		max-width: 100%;
		height: auto;
		border-radius: 6px;
	}

	.content.no-images :global(img) {
		display: none;
	}

	.content :global(h1),
	.content :global(h2),
	.content :global(h3),
	.content :global(h4) {
		margin: 1.3em 0 0.5em;
		font-weight: 600;
		line-height: 1.3;
	}

	.content :global(h1) {
		font-size: 1.3em;
	}

	.content :global(h2) {
		font-size: 1.18em;
	}

	.content :global(h3),
	.content :global(h4) {
		font-size: 1.05em;
	}

	.content :global(blockquote) {
		margin: 1em 0;
		padding: 0 0 0 16px;
		border-left: 3px solid var(--line);
		color: var(--text-muted);
		font-style: italic;
	}

	.content :global(pre) {
		padding: 12px 14px;
		overflow-x: auto;
		border-radius: var(--radius);
		background: var(--code-bg);
		font-size: 13.5px;
		line-height: 1.5;
	}

	.content :global(code) {
		font-family: var(--font-mono);
		font-size: 0.86em;
	}

	.content :global(pre code) {
		font-size: inherit;
	}

	.content :global(hr) {
		border: 0;
		border-top: 1px solid var(--line);
		margin: 1.5em 0;
	}

	.content :global(table) {
		display: block;
		max-width: 100%;
		overflow-x: auto;
		border-collapse: collapse;
		font-family: var(--font);
		font-size: 14px;
	}

	.content :global(th),
	.content :global(td) {
		padding: 5px 9px;
		border: 1px solid var(--line);
	}

	.content :global(figure) {
		margin: 1.2em 0;
	}

	.content :global(figcaption) {
		margin-top: 6px;
		font-family: var(--font);
		font-size: 13px;
		color: var(--text-faint);
	}

	@media (max-width: 699px) {
		.source {
			padding-left: 14px;
		}

		.title {
			padding: 14px 16px 0;
			font-size: 21px;
		}

		.content {
			padding: 10px 16px 20px;
			font-size: 17px;
		}
	}
</style>
