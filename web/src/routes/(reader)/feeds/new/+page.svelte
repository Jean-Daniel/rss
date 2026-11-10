<script lang="ts">
	import { untrack } from 'svelte';
	import { page } from '$app/state';
	import { api } from '#lib/api/client.ts';
	import type { AddFeedResponse, FeedChoice } from '#lib/api/types.ts';
	import Checklist from '#lib/components/Checklist.svelte';
	import { afterSave, failure } from '#lib/manage.ts';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();
	const subs = $derived(data.subscriptions);

	// /feeds/new?url=… starts with that address.
	let address = $state(untrack(() => page.url.searchParams.get('url') ?? ''));
	let groupIds = $state<number[]>([]);
	let needsLogin = $state(false);
	let username = $state('');
	let password = $state('');

	let busy = $state(false);
	let error = $state('');
	let existing = $state<number | null>(null);
	// The feeds a web page links to, when it links to several.
	let choices = $state<FeedChoice[] | null>(null);
	let chosen = $state('');

	const existingFeed = $derived(subs.feeds.find((f) => f.id === existing));

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		existing = null;
		try {
			const res = await api<AddFeedResponse>('/api/feeds', {
				method: 'POST',
				body: JSON.stringify({
					url: choices ? chosen : address,
					groupIds,
					username: needsLogin ? username : null,
					password: needsLogin ? password : null
				})
			});
			if (res.choices?.length) {
				choices = res.choices;
				chosen = res.choices[0].url;
				return;
			}
			if (res.existing) {
				existing = res.feedId;
				return;
			}
			await afterSave(`/feeds/${res.feedId}`);
		} catch (err) {
			error = failure(err);
		} finally {
			busy = false;
		}
	}

	function addressChanged() {
		choices = null;
		existing = null;
		error = '';
	}
</script>

<form class="panel" onsubmit={submit}>
	<h2>Add a feed</h2>
	<label class="field">
		<span>Address of the feed, or of the web site</span>
		<!-- type="text": addresses without http:// are accepted too. -->
		<input
			type="text"
			inputmode="url"
			autocomplete="url"
			autocapitalize="none"
			spellcheck="false"
			placeholder="https://example.com"
			required
			bind:value={address}
			oninput={addressChanged}
		/>
	</label>

	{#if choices}
		<fieldset>
			<legend>This page links to several feeds. Which one?</legend>
			{#each choices as choice (choice.url)}
				<label class="check">
					<input type="radio" name="choice" value={choice.url} bind:group={chosen} />
					<span>
						{choice.title || 'Untitled feed'}<br />
						<small>{choice.url}</small>
					</span>
				</label>
			{/each}
		</fieldset>
	{/if}

	<Checklist
		legend="Groups"
		items={subs.groups.map((g) => ({ id: g.id, label: g.title }))}
		bind:selected={groupIds}
		empty="No groups yet. The feed is listed under All items."
	/>

	<label class="check">
		<input type="checkbox" bind:checked={needsLogin} />
		<span>The feed needs a username and password</span>
	</label>
	{#if needsLogin}
		<p class="hint">Leave the username empty for feeds that only want a password or a token.</p>
		<label class="field">
			<span>Username</span>
			<input type="text" autocomplete="off" autocapitalize="none" bind:value={username} />
		</label>
		<label class="field">
			<span>Password</span>
			<input type="password" autocomplete="new-password" bind:value={password} />
		</label>
	{/if}

	{#if error}<p class="message error" role="alert">{error}</p>{/if}
	{#if existing !== null}
		<p class="message" role="status">
			You already subscribe to this feed:
			<a href="/feeds/{existing}">{existingFeed?.title ?? 'open it'}</a>.
		</p>
	{/if}

	<div class="buttons">
		<button class="button primary" disabled={busy || address.trim() === ''}>
			{busy ? 'Looking for the feed…' : choices ? 'Add this feed' : 'Add'}
		</button>
		<a class="button" href="/settings">Cancel</a>
	</div>
</form>

<style>
	small {
		color: var(--text-faint);
		overflow-wrap: anywhere;
	}
</style>
