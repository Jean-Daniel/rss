<script lang="ts">
	import { untrack } from 'svelte';
	import { api } from '#lib/api/client.ts';
	import type { Created, Group, GroupUpdate, Subscriptions } from '#lib/api/types.ts';
	import { afterDelete, afterSave, failure } from '#lib/manage.ts';
	import Checklist from './Checklist.svelte';
	import DisplayOptionsFields from './DisplayOptionsFields.svelte';

	/** group is null for a new one. */
	let { group, subs }: { group: Group | null; subs: Subscriptions } = $props();

	const start = untrack(() => group);
	let form = $state<GroupUpdate>({
		title: start?.title ?? '',
		feedIds: start ? [...start.feedIds] : [],
		showImages: start?.showImages ?? null,
		oldestFirst: start?.oldestFirst ?? null,
		showUnreadCount: start?.showUnreadCount ?? null
	});
	let busy = $state(false);
	let error = $state('');

	async function save(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		try {
			let id = group?.id;
			if (id === undefined) {
				id = (await api<Created>('/api/groups', { method: 'POST', body: JSON.stringify(form) })).id;
			} else {
				await api(`/api/groups/${id}`, { method: 'PUT', body: JSON.stringify(form) });
			}
			await afterSave(`/groups/${id}`);
		} catch (err) {
			error = failure(err);
		} finally {
			busy = false;
		}
	}

	async function remove() {
		if (!group || !confirm(`Delete the group ${group.title}? Its feeds stay.`)) return;
		busy = true;
		error = '';
		try {
			await api(`/api/groups/${group.id}`, { method: 'DELETE' });
			await afterDelete('/');
		} catch (err) {
			error = failure(err);
			busy = false;
		}
	}
</script>

<form class="panel" onsubmit={save}>
	<h2>{group ? 'Group settings' : 'Add a group'}</h2>
	<label class="field">
		<span>Name</span>
		<input type="text" required bind:value={form.title} />
	</label>

	<Checklist
		legend="Feeds"
		items={subs.feeds.map((f) => ({ id: f.id, label: f.title }))}
		bind:selected={form.feedIds}
		empty="No feeds yet."
	/>

	<DisplayOptionsFields
		bind:showImages={form.showImages}
		bind:oldestFirst={form.oldestFirst}
		bind:showUnreadCount={form.showUnreadCount}
		defaults={subs.defaults}
		forFeed={false}
	/>

	{#if error}<p class="message error" role="alert">{error}</p>{/if}
	<div class="buttons">
		<button class="button primary" disabled={busy || form.title.trim() === ''}>
			{group ? 'Save' : 'Add'}
		</button>
		<a class="button" href={group ? `/groups/${group.id}` : '/settings'}>Cancel</a>
		{#if group}
			<button type="button" class="button danger end" disabled={busy} onclick={remove}>
				Delete group
			</button>
		{/if}
	</div>
</form>
