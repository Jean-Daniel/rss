<script lang="ts">
	import type { Snippet } from 'svelte';

	let { children }: { children?: Snippet } = $props();
</script>

<div class="empty">
	{#if children}
		{@render children()}
	{:else}
		<p>No items to show yet.</p>
	{/if}
</div>

<style>
	.empty {
		padding: 28px 24px;
		border-radius: var(--radius);
		background: var(--surface);
		box-shadow: var(--shadow);
		color: var(--text-muted);
	}

	.empty :global(p) {
		margin: 0;
	}

	.empty :global(p + p) {
		margin-top: 8px;
	}
</style>
