<script lang="ts">
	import { page } from '$app/state';
</script>

<svelte:head>
	<title>{page.status === 503 ? 'Offline' : 'Error'} · Fever</title>
</svelte:head>

<main>
	<div class="card">
		<p class="logo">fever<span>°</span></p>
		{#if page.status === 404}
			<h1>This page does not exist.</h1>
			<p><a href="/">Back to all items</a></p>
		{:else}
			<h1>{page.status === 503 ? 'The server cannot be reached.' : 'Something went wrong.'}</h1>
			{#if page.error?.message}<p class="detail">{page.error.message}</p>{/if}
			<button type="button" onclick={() => location.reload()}>Try again</button>
		{/if}
	</div>
</main>

<style>
	main {
		display: grid;
		place-items: center;
		min-height: 100dvh;
		padding: 16px;
	}

	.card {
		width: min(100%, 380px);
		padding: 28px 24px 24px;
		border-radius: var(--radius);
		background: var(--surface);
		box-shadow: var(--shadow);
		text-align: center;
	}

	.logo {
		margin: 0 0 12px;
		font-size: 28px;
		font-weight: bold;
		letter-spacing: -0.02em;
	}

	.logo span {
		color: var(--accent-strong);
	}

	h1 {
		margin: 0 0 8px;
		font-size: 17px;
	}

	.detail {
		margin: 0 0 16px;
		color: var(--text-muted);
	}

	button {
		height: 40px;
		padding: 0 20px;
		border: 0;
		border-radius: 4px;
		background: var(--row-selected);
		color: #fff;
		font-weight: bold;
		cursor: pointer;
	}
</style>
