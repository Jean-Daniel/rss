<script lang="ts">
	import Stream from '#lib/components/Stream.svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();
</script>

{#key data.stream}
	<Stream stream={data.stream} subs={data.subscriptions} />
{/key}
