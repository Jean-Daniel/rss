<script lang="ts">
	import { untrack } from 'svelte';
	import { page } from '$app/state';
	import { api } from '#lib/api/client.ts';
	import type { Feed, FeedSettings, FeedUpdate, Subscriptions } from '#lib/api/types.ts';
	import { staleText } from '#lib/feed-status.ts';
	import { afterDelete, afterSave, failure } from '#lib/manage.ts';
	import { formatDate } from '#lib/time.ts';
	import Checklist from './Checklist.svelte';
	import DisplayOptionsFields from './DisplayOptionsFields.svelte';

	let { settings, feed, subs }: { settings: FeedSettings; feed: Feed; subs: Subscriptions } =
		$props();

	// The form starts from the settings as loaded (the page re-creates it for
	// another feed).
	const start = untrack(() => settings);
	let form = $state<FeedUpdate>({
		title: start.title,
		url: start.url,
		groupIds: [...start.groupIds],
		showImages: start.showImages,
		oldestFirst: start.oldestFirst,
		showUnreadCount: start.showUnreadCount,
		username: start.username,
		password: null
	});
	let needsLogin = $state(start.username !== null);
	let newPassword = $state('');

	let busy = $state(false);
	let error = $state('');

	// Back to the feed's list, in the group it was opened from.
	const group = $derived(Number(page.url.searchParams.get('group')) || null);
	const back = $derived(group ? `/feeds/${feed.id}?group=${group}` : `/feeds/${feed.id}`);

	async function save(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		const body: FeedUpdate = {
			...form,
			// An empty username is a login too: some feeds only want a password.
			username: needsLogin ? (form.username ?? '') : null,
			// Left empty: the stored password stays.
			password: needsLogin && newPassword !== '' ? newPassword : needsLogin ? null : ''
		};
		try {
			await api(`/api/feeds/${feed.id}`, { method: 'PUT', body: JSON.stringify(body) });
			await afterSave(back);
		} catch (err) {
			error = failure(err);
		} finally {
			busy = false;
		}
	}

	async function remove() {
		const items = settings.itemCount === 1 ? '1 item' : `${settings.itemCount} items`;
		const saved = settings.savedCount
			? `, including ${settings.savedCount} saved ${settings.savedCount === 1 ? 'one' : 'ones'}`
			: '';
		if (!confirm(`Unsubscribe from ${feed.title}? Its ${items} will be deleted${saved}.`)) return;
		busy = true;
		error = '';
		try {
			await api(`/api/feeds/${feed.id}`, { method: 'DELETE' });
			await afterDelete(group ? `/groups/${group}` : '/');
		} catch (err) {
			error = failure(err);
			busy = false;
		}
	}
</script>

<form class="panel" onsubmit={save}>
	<h2>Feed settings</h2>

	<div class="state" class:problem={feed.error !== null}>
		{#if feed.error}
			<p><strong>{feed.error}</strong></p>
		{:else if feed.stale}
			<p><strong>{staleText(feed, subs.retentionWeeks)}.</strong></p>
		{:else if feed.lastUpdatedAt}
			<p>New items on {formatDate(feed.lastUpdatedAt)}.</p>
		{/if}
		<p>
			{settings.itemCount === 1 ? '1 item' : `${settings.itemCount} items`}{settings.savedCount
				? `, ${settings.savedCount} saved`
				: ''}.
			{#if feed.siteUrl}
				Site: <a href={feed.siteUrl} target="_blank" rel="noopener noreferrer">{feed.siteUrl}</a>
			{/if}
		</p>
	</div>

	<label class="field">
		<span>Name</span>
		<input type="text" placeholder="The feed's own title" bind:value={form.title} />
	</label>
	<p class="hint">Leave it empty to use the title the feed gives itself.</p>
	<label class="field">
		<span>Address of the feed</span>
		<input
			type="text"
			inputmode="url"
			autocapitalize="none"
			spellcheck="false"
			required
			bind:value={form.url}
		/>
	</label>

	<Checklist
		legend="Groups"
		items={subs.groups.map((g) => ({ id: g.id, label: g.title }))}
		bind:selected={form.groupIds}
		empty="No groups yet."
	/>

	<DisplayOptionsFields
		bind:showImages={form.showImages}
		bind:oldestFirst={form.oldestFirst}
		bind:showUnreadCount={form.showUnreadCount}
		defaults={subs.defaults}
		forFeed
	/>

	<label class="check">
		<input type="checkbox" bind:checked={needsLogin} />
		<span>The feed needs a username and password</span>
	</label>
	{#if needsLogin}
		<p class="hint">Leave the username empty for feeds that only want a password or a token.</p>
		<label class="field">
			<span>Username</span>
			<input type="text" autocomplete="off" autocapitalize="none" bind:value={form.username} />
		</label>
		<label class="field">
			<span>Password</span>
			<input
				type="password"
				autocomplete="new-password"
				placeholder={settings.hasPassword ? 'Unchanged' : ''}
				bind:value={newPassword}
			/>
		</label>
	{/if}

	{#if error}<p class="message error" role="alert">{error}</p>{/if}
	<div class="buttons">
		<button class="button primary" disabled={busy}>Save</button>
		<a class="button" href={back}>Cancel</a>
		<button type="button" class="button danger end" disabled={busy} onclick={remove}>
			Unsubscribe
		</button>
	</div>
</form>

<style>
	.state {
		margin-bottom: 14px;
		padding: 8px 12px;
		border-radius: var(--radius);
		background: var(--row);
		font-size: 13px;
		overflow-wrap: anywhere;
	}

	.state.problem {
		background: #fbeee4;
		color: #8a4b1d;
	}

	.state p {
		margin: 0;
	}

	.field + .hint {
		margin-top: -8px;
	}
</style>
