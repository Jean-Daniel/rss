<script lang="ts">
	import type { Feed, Group } from '#lib/api/types.ts';
	import { staleText } from '#lib/feed-status.ts';
	import type { Selection } from '#lib/selection.ts';
	import FeedIcon from './FeedIcon.svelte';

	let {
		feeds,
		group,
		selection,
		retentionWeeks
	}: { feeds: Feed[]; group: Group | null; selection: Selection; retentionWeeks: number } =
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
						title={feed.error ?? (feed.stale ? staleText(feed, retentionWeeks) : feed.url)}
						aria-current={(selection.kind === 'feed' || selection.kind === 'manage') &&
						selection.feed?.id === feed.id
							? 'page'
							: undefined}
					>
						<FeedIcon faviconId={feed.faviconId} />
						<span class="label">{feed.title}</span>
						{#if feed.error !== null}
							<span class="warning" aria-label="Problem with this feed">!</span>
						{:else}
							{#if feed.stale}
								<!-- Nothing new for the retention period: the feed may be abandoned. -->
								<svg
									class="stale"
									viewBox="0 0 16 16"
									role="img"
									aria-label={staleText(feed, retentionWeeks)}
								>
									<circle cx="8" cy="8" r="6.25" />
									<path d="M8 4.75V8l2.25 1.5" />
								</svg>
							{/if}
							<!-- The feed's option, else the group's it is listed in. -->
							{#if feed.unread > 0 && (feed.showUnreadCount ?? group?.showUnreadCount) !== false}
								<span class="count">{feed.unread}</span>
							{/if}
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

	.stale {
		flex: none;
		width: 14px;
		height: 14px;
		fill: none;
		stroke: var(--text-faint);
		stroke-width: 1.5;
		stroke-linecap: round;
		stroke-linejoin: round;
	}

	[aria-current='page'] .stale {
		stroke: rgb(255 255 255 / 0.75);
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
