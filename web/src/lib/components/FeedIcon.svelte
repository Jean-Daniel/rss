<script lang="ts">
	let { faviconId }: { faviconId: number | null } = $props();
	let failed = $state(false);
</script>

{#if faviconId !== null && !failed}
	<img
		class="favicon"
		src="/api/favicons/{faviconId}"
		width="16"
		height="16"
		alt=""
		loading="lazy"
		decoding="async"
		onerror={() => (failed = true)}
	/>
{:else}
	<span class="favicon placeholder" aria-hidden="true"></span>
{/if}

<style>
	.favicon {
		flex: none;
		width: 16px;
		height: 16px;
		border-radius: 3px;
	}

	.placeholder {
		background: radial-gradient(circle at 50% 50%, #c8c8c8 0 5px, transparent 5.5px);
	}

	:global(a.read) .favicon {
		opacity: 0.7;
	}
</style>
