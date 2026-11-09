<script lang="ts">
	import Stream from '#lib/components/Stream.svelte';
	import { staleText } from '#lib/feed-status.ts';
	import type { PageProps } from './$types';

	let { data, params }: PageProps = $props();

	const feed = $derived(data.subscriptions.feeds.find((f) => f.id === Number(params.id)));
</script>

{#if feed && (feed.error || feed.stale)}
	<div class="notice" class:problem={feed.error}>
		{#if feed.error}
			<p>{feed.error}</p>
		{:else}
			<p>
				{staleText(feed, data.subscriptions.retentionWeeks)}. The site may have stopped publishing,
				or moved its feed.
			</p>
		{/if}
		<p class="source">
			{#if feed.siteUrl}
				<a href={feed.siteUrl} target="_blank" rel="noopener noreferrer">{feed.siteUrl}</a> ·
			{/if}
			<span>{feed.url}</span>
		</p>
	</div>
{/if}

{#key data.stream}
	<Stream stream={data.stream} subs={data.subscriptions} />
{/key}

<style>
	.notice {
		margin-bottom: 12px;
		padding: 10px 14px;
		border-radius: var(--radius);
		background: #f6f1e7;
		color: #6b5418;
	}

	.notice.problem {
		background: #fbeee4;
		color: #8a4b1d;
	}

	.notice p {
		margin: 0;
		font-weight: bold;
	}

	.notice .source {
		margin-top: 4px;
		font-size: 12px;
		font-weight: normal;
		overflow-wrap: anywhere;
	}
</style>
