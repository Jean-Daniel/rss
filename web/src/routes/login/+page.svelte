<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '#lib/api/client.ts';
	import { safeNext } from '#lib/navigation.ts';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	let password = $state('');
	let error = $state('');
	let busy = $state(false);

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		try {
			await api('/api/session', { method: 'POST', body: JSON.stringify({ password }) });
			// Back to the page that asked for the login, if any.
			await goto(safeNext(page.url.searchParams.get('next')), { refreshAll: true });
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not reach the server.';
			password = '';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head>
	<title>Log in · Fever</title>
</svelte:head>

<main>
	<form class="card" onsubmit={submit}>
		<h1 class="logo">fever<span>°</span></h1>

		{#if data.unavailable}
			<p class="notice">{data.unavailable.message}</p>
		{:else if data.session?.passwordSet === false}
			<p class="notice">
				No password is set yet. Run <code>fever set-password</code> on the server first.
			</p>
		{/if}

		<!-- Lets password managers store the password under a stable name. -->
		<input
			class="visually-hidden"
			type="text"
			name="username"
			autocomplete="username"
			value="fever"
			readonly
			tabindex="-1"
			aria-hidden="true"
		/>

		<label for="password">Password</label>
		<input
			id="password"
			name="password"
			type="password"
			autocomplete="current-password"
			required
			bind:value={password}
			{@attach (node) => node.focus()}
		/>

		{#if error}
			<p class="error" role="alert">{error}</p>
		{/if}

		<button type="submit" disabled={busy}>{busy ? 'Logging in…' : 'Log in'}</button>
	</form>
</main>

<style>
	main {
		display: grid;
		place-items: center;
		min-height: 100dvh;
		padding: 16px;
	}

	.card {
		display: grid;
		gap: 10px;
		width: min(100%, 340px);
		padding: 28px 24px 24px;
		border-radius: var(--radius);
		background: var(--surface);
		box-shadow: var(--shadow);
	}

	.logo {
		margin: 0 0 8px;
		font-size: 28px;
		letter-spacing: -0.02em;
		text-align: center;
	}

	.logo span {
		color: var(--accent-strong);
	}

	label {
		font-weight: bold;
	}

	input[type='password'] {
		height: 40px;
		padding: 0 10px;
		border: 1px solid #c8c8c8;
		border-radius: 4px;
		font: inherit;
		font-size: 16px; /* avoids the iOS zoom on focus */
	}

	button {
		height: 40px;
		margin-top: 6px;
		border: 0;
		border-radius: 4px;
		background: var(--row-selected);
		color: #fff;
		font-weight: bold;
		cursor: pointer;
	}

	button:disabled {
		opacity: 0.6;
		cursor: default;
	}

	.notice,
	.error {
		margin: 0;
		padding: 8px 10px;
		border-radius: 4px;
		font-size: 13px;
	}

	.notice {
		background: #fdf6e3;
		color: #6b5418;
	}

	.error {
		background: #fbe9e7;
		color: var(--accent);
	}
</style>
