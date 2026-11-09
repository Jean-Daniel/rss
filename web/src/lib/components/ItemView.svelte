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
		onToggleRead
	}: {
		item: Item;
		feed: Feed | undefined;
		feedHref: string;
		showImages: boolean;
		now: number;
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
	aria-labelledby="item-{item.id}"
>
	<header>
		<h2 class="title" id="item-{item.id}">
			{#if item.url}
				<!-- Opening the article counts as reading it (also with a middle click). -->
				<a
					href={item.url}
					target="_blank"
					rel="noopener noreferrer"
					onclick={() => itemState.markRead(item)}
					onauxclick={(e) => e.button === 1 && itemState.markRead(item)}
					>{item.title || 'Untitled'}</a
				>
			{:else}
				{item.title || 'Untitled'}
			{/if}
		</h2>
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
	<!-- Sanitized on the server (internal/content). -->
	<div class="content" class:no-images={!showImages}>{@html item.content}</div>
</article>

<style>
	.item {
		margin-bottom: 16px;
		border-radius: var(--radius);
		background: var(--surface);
		box-shadow: var(--shadow);
		overflow: hidden;
	}

	header {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		grid-template-areas: 'title actions' 'meta actions';
		gap: 2px 8px;
		padding: 14px 12px 10px 18px;
		border-bottom: 1px solid #ececec;
	}

	.title {
		grid-area: title;
		margin: 0;
		font-size: 18px;
		line-height: 1.3;
		overflow-wrap: anywhere;
	}

	.title a {
		color: var(--text);
	}

	.title a:hover {
		color: var(--accent);
	}

	.read .title,
	.read .title a {
		font-weight: normal;
		color: var(--text-muted);
	}

	.meta {
		grid-area: meta;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 2px 10px;
		font-size: 12px;
		color: var(--text-faint);
	}

	.feed {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		min-width: 0;
		color: var(--text-muted);
	}

	.feed span {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.actions {
		grid-area: actions;
		display: flex;
		align-items: flex-start;
	}

	.action {
		display: grid;
		place-items: center;
		width: 36px;
		height: 36px;
		padding: 0;
		border: 0;
		border-radius: var(--radius);
		background: transparent;
		color: #b5b5b5;
		cursor: pointer;
	}

	.action:hover {
		background: var(--row-hover);
		color: var(--text-muted);
	}

	.action svg {
		width: 18px;
		height: 18px;
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

	/* Article text. */
	.content {
		padding: 14px 18px 18px;
		font-size: 15px;
		line-height: 1.6;
		overflow-wrap: break-word;
	}

	.read .content {
		color: #555;
	}

	.content :global(:first-child) {
		margin-top: 0;
	}

	.content :global(:last-child) {
		margin-bottom: 0;
	}

	.content :global(a) {
		font-weight: normal;
		color: var(--accent);
		text-decoration: underline;
		text-decoration-color: rgb(170 17 0 / 0.3);
		text-underline-offset: 2px;
	}

	.content :global(img) {
		max-width: 100%;
		height: auto;
	}

	.content.no-images :global(img) {
		display: none;
	}

	.content :global(h1),
	.content :global(h2),
	.content :global(h3),
	.content :global(h4) {
		margin: 1.2em 0 0.5em;
		line-height: 1.3;
	}

	.content :global(h1) {
		font-size: 1.35em;
	}

	.content :global(h2) {
		font-size: 1.2em;
	}

	.content :global(h3),
	.content :global(h4) {
		font-size: 1.05em;
	}

	.content :global(blockquote) {
		margin: 1em 0;
		padding: 0 0 0 14px;
		border-left: 3px solid #ddd;
		color: var(--text-muted);
	}

	.content :global(pre) {
		padding: 10px 12px;
		overflow-x: auto;
		border-radius: 4px;
		background: #f4f4f4;
		font-size: 13px;
		line-height: 1.45;
	}

	.content :global(code) {
		font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
		font-size: 0.9em;
	}

	.content :global(table) {
		display: block;
		max-width: 100%;
		overflow-x: auto;
		border-collapse: collapse;
	}

	.content :global(th),
	.content :global(td) {
		padding: 4px 8px;
		border: 1px solid #e3e3e3;
	}

	.content :global(figure) {
		margin: 1em 0;
	}

	.content :global(figcaption) {
		font-size: 13px;
		color: var(--text-faint);
	}

	@media (max-width: 699px) {
		header {
			padding: 12px 8px 8px 14px;
		}

		.content {
			padding: 12px 14px 16px;
			font-size: 16px;
		}
	}
</style>
