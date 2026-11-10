<script lang="ts">
	import GroupForm from '#lib/components/GroupForm.svelte';
	import type { PageProps } from './$types';

	let { data, params }: PageProps = $props();
	const group = $derived(data.subscriptions.groups.find((g) => g.id === Number(params.id)));
</script>

{#if group}
	{#key group.id}
		<GroupForm {group} subs={data.subscriptions} />
	{/key}
{:else}
	<p class="panel">This group does not exist any more.</p>
{/if}
