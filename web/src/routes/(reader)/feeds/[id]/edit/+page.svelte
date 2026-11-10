<script lang="ts">
	import FeedForm from '#lib/components/FeedForm.svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();
	const feed = $derived(data.subscriptions.feeds.find((f) => f.id === data.settings.id));
</script>

{#if feed}
	{#key data.settings}
		<FeedForm settings={data.settings} {feed} subs={data.subscriptions} />
	{/key}
{:else}
	<p class="panel">This feed does not exist any more.</p>
{/if}
