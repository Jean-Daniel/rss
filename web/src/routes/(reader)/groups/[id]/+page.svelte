<script lang="ts">
	import EmptyStream from '#lib/components/EmptyStream.svelte';
	import type { PageProps } from './$types';

	let { data, params }: PageProps = $props();

	const group = $derived(data.subscriptions.groups.find((g) => g.id === Number(params.id)));
</script>

{#if group}
	<EmptyStream />
{:else}
	<EmptyStream>
		<p>This group does not exist any more. <a href="/">Back to all items</a></p>
	</EmptyStream>
{/if}
