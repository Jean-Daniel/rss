<script lang="ts">
	import type { Feed, Group } from '#lib/api/types.ts';
	import type { Selection } from '#lib/selection.ts';
	import FeedIcon from './FeedIcon.svelte';

	let { feeds, group, selection }: { feeds: Feed[]; group: Group | null; selection: Selection } =
		$props();

	const href = (feed: Feed) =>
		group ? `/feeds/${feed.id}?group=${group.id}` : `/feeds/${feed.id}`;
</script>

<section aria-label={group ? `Feeds in ${group.title}` : 'All feeds'}>
	<h2 class="heading">{group ? group.title : 'All feeds'}</h2>
	{#if feeds.length === 0}
		<p class="empty box">No feeds here.</p>
	{:else}
		<ul class="rows box">
			{#each feeds as feed (feed.id)}
				<li>
					<a
						href={href(feed)}
						class:read={feed.unread === 0}
						class:failing={feed.error !== null}
						title={feed.error ?? feed.url}
						aria-current={selection.kind === 'feed' && selection.feed.id === feed.id
							? 'page'
							: undefined}
					>
						<FeedIcon faviconId={feed.faviconId} />
						<span class="label">{feed.title}</span>
						{#if feed.error !== null}
							<span class="warning" aria-label="Problem with this feed">!</span>
						{:else if feed.unread > 0 && feed.showUnreadCount !== false}
							<span class="count">{feed.unread}</span>
						{/if}
					</a>
				</li>
			{/each}
		</ul>
	{/if}
</section>

<style>
	.heading {
		margin: 0 0 8px;
		padding: 0 4px;
		font-size: 11px;
		font-weight: bold;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--text-faint);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.empty {
		margin: 0;
		padding: 10px;
		color: var(--text-muted);
	}

	.failing {
		color: #a26731;
	}

	.warning {
		display: inline-grid;
		place-items: center;
		width: 16px;
		height: 16px;
		border-radius: 50%;
		background: #c9853f;
		color: #fff;
		font-size: 11px;
	}
</style>
