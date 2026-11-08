<script lang="ts">
	import EmptyStream from '#lib/components/EmptyStream.svelte';
	import type { PageProps } from './$types';

	let { data, params }: PageProps = $props();

	const feed = $derived(data.subscriptions.feeds.find((f) => f.id === Number(params.id)));
</script>

{#if feed}
	<EmptyStream>
		{#if feed.error}
			<p class="problem">{feed.error}</p>
		{/if}
		<p>No items to show yet.</p>
		<p class="source">
			{#if feed.siteUrl}
				<a href={feed.siteUrl} target="_blank" rel="noopener noreferrer">{feed.siteUrl}</a> ·
			{/if}
			<span>{feed.url}</span>
		</p>
	</EmptyStream>
{:else}
	<EmptyStream>
		<p>This feed does not exist any more. <a href="/">Back to all items</a></p>
	</EmptyStream>
{/if}

<style>
	.problem {
		color: #a26731;
		font-weight: bold;
	}

	.source {
		font-size: 12px;
		overflow-wrap: anywhere;
	}
</style>
