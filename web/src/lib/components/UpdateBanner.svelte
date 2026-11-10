<script lang="ts">
	import { updated } from '$app/state';

	// SvelteKit checks for a new version when the app comes back to the
	// foreground and every hour. Reading is not interrupted: the reader
	// reloads when it suits them (pending reads are kept and sent).
</script>

{#if updated.current}
	<div class="update" role="status">
		<span>A new version of Fever is available.</span>
		<button onclick={() => location.reload()}>Reload</button>
	</div>
{/if}

<style>
	.update {
		position: fixed;
		left: 50%;
		bottom: calc(16px + env(safe-area-inset-bottom));
		/* Above the items and the phone's top bar, below the drawer and
		   overlays (and their backdrop), which would otherwise lose their
		   last rows under it. */
		z-index: 12;
		display: flex;
		align-items: center;
		gap: 12px;
		max-width: calc(100vw - 32px);
		padding: 10px 10px 10px 16px;
		transform: translateX(-50%);
		border-radius: var(--radius-card);
		background: var(--button-strong);
		box-shadow: var(--shadow-float);
		color: var(--button-strong-text);
		font-size: 13px;
	}

	button {
		flex: none;
		height: 32px;
		padding: 0 14px;
		border: 0;
		border-radius: var(--radius);
		background: var(--button-strong-text);
		color: var(--button-strong);
		font-weight: 600;
		cursor: pointer;
	}
</style>
