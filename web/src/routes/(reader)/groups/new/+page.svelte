<script lang="ts">
	import GroupForm from '#lib/components/GroupForm.svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();
</script>

<GroupForm group={null} subs={data.subscriptions} />
