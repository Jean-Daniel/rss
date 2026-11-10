<script lang="ts">
	import { untrack } from 'svelte';
	import { api } from '#lib/api/client.ts';
	import type { OPMLImport, Preferences } from '#lib/api/types.ts';
	import FeedIcon from '#lib/components/FeedIcon.svelte';
	import { staleText } from '#lib/feed-status.ts';
	import { afterSave, failure } from '#lib/manage.ts';
	import { formatDate } from '#lib/time.ts';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();
	const subs = $derived(data.subscriptions);

	// Reading preferences, seeded once from the server's.
	const loaded = untrack(() => data.subscriptions.defaults);
	let prefs = $state<Preferences>({
		oldestFirst: loaded.oldestFirst,
		showImages: loaded.showImages,
		showRead: loaded.showRead,
		autoRead: loaded.autoRead
	});
	let prefsBusy = $state(false);
	let prefsMessage = $state<{ ok: boolean; text: string } | null>(null);

	async function savePrefs(event: SubmitEvent) {
		event.preventDefault();
		prefsBusy = true;
		prefsMessage = null;
		try {
			await api('/api/preferences', { method: 'PUT', body: JSON.stringify(prefs) });
			await afterSave();
			prefsMessage = { ok: true, text: 'Saved. Lists use them from the next one you open.' };
		} catch (err) {
			prefsMessage = { ok: false, text: failure(err) };
		} finally {
			prefsBusy = false;
		}
	}

	const feedsOfGroup = (n: number) => (n === 1 ? '1 feed' : `${n} feeds`);

	function feedStatus(feed: (typeof subs.feeds)[number]): string {
		if (feed.error) return feed.error;
		if (feed.stale) return staleText(feed, subs.retentionWeeks);
		if (feed.lastUpdatedAt) return `New items on ${formatDate(feed.lastUpdatedAt)}`;
		return 'Not fetched yet';
	}

	// OPML import.
	let file = $state<File | null>(null);
	let importBusy = $state(false);
	let importMessage = $state<{ ok: boolean; text: string } | null>(null);

	function plural(n: number, one: string, many: string) {
		return `${n} ${n === 1 ? one : many}`;
	}

	async function importOPML(event: SubmitEvent) {
		event.preventDefault();
		if (!file) return;
		importBusy = true;
		importMessage = null;
		try {
			const res = await api<OPMLImport>('/api/opml', {
				method: 'POST',
				headers: { 'Content-Type': file.type || 'text/x-opml' },
				body: file
			});
			const parts = [
				`Added ${plural(res.feedsAdded, 'feed', 'feeds')}` +
					(res.groupsAdded ? ` and ${plural(res.groupsAdded, 'group', 'groups')}` : '') +
					'.'
			];
			if (res.feedsKnown) {
				parts.push(`${plural(res.feedsKnown, 'feed was', 'feeds were')} already there.`);
			}
			if (res.skipped) {
				parts.push(
					`${plural(res.skipped, 'entry was', 'entries were')} skipped: not a web address.`
				);
			}
			if (res.feedsAdded) parts.push('New feeds are being fetched.');
			importMessage = { ok: true, text: parts.join(' ') };
			await afterSave();
		} catch (err) {
			importMessage = { ok: false, text: failure(err) };
		} finally {
			importBusy = false;
		}
	}
</script>

<section class="panel">
	<h2>Feeds and groups</h2>
	<div class="buttons add">
		<a class="button primary" href="/feeds/new">Add a feed</a>
		<a class="button" href="/groups/new">Add a group</a>
	</div>

	{#if subs.groups.length > 0}
		<h3>Groups</h3>
		<ul class="rows box">
			{#each subs.groups as group (group.id)}
				<li>
					<a href="/groups/{group.id}/edit">
						<span class="label">{group.title}</span>
						<span class="count">{feedsOfGroup(group.feedIds.length)}</span>
					</a>
				</li>
			{/each}
		</ul>
	{/if}

	{#if subs.feeds.length > 0}
		<h3>Feeds</h3>
		<ul class="rows box feeds">
			{#each subs.feeds as feed (feed.id)}
				<li>
					<a href="/feeds/{feed.id}/edit" class:failing={feed.error !== null}>
						<FeedIcon faviconId={feed.faviconId} />
						<span class="label">{feed.title}</span>
						<span class="status">{feedStatus(feed)}</span>
					</a>
				</li>
			{/each}
		</ul>
	{/if}
</section>

<form class="panel" onsubmit={savePrefs}>
	<h2>Reading</h2>
	<p class="hint">Used where a group or feed does not set its own display options.</p>
	<label class="field">
		<span>Order</span>
		<select
			value={prefs.oldestFirst ? 'oldest' : 'newest'}
			onchange={(e) => (prefs.oldestFirst = e.currentTarget.value === 'oldest')}
		>
			<option value="oldest">Oldest first</option>
			<option value="newest">Newest first</option>
		</select>
	</label>
	<label class="field">
		<span>Articles</span>
		<select
			value={prefs.showImages ? 'images' : 'text'}
			onchange={(e) => (prefs.showImages = e.currentTarget.value === 'images')}
		>
			<option value="images">With images</option>
			<option value="text">Text only</option>
		</select>
	</label>
	<label class="field">
		<span>Lists show</span>
		<select
			value={prefs.showRead ? 'all' : 'unread'}
			onchange={(e) => (prefs.showRead = e.currentTarget.value === 'all')}
		>
			<option value="unread">Unread items</option>
			<option value="all">All items</option>
		</select>
	</label>
	<p class="hint">
		On each device, the Unread and All buttons above a list change this, and the device keeps its
		choice.
	</p>
	<label class="check">
		<input type="checkbox" bind:checked={prefs.autoRead} />
		<span>Mark items as read as they scroll past</span>
	</label>
	{#if prefsMessage}
		<p class="message" class:ok={prefsMessage.ok} class:error={!prefsMessage.ok} role="status">
			{prefsMessage.text}
		</p>
	{/if}
	<div class="buttons">
		<button class="button primary" disabled={prefsBusy}>Save</button>
	</div>
</form>

<section class="panel">
	<h2>Import and export</h2>
	<p class="hint">
		OPML files move feeds and groups between feed readers. Importing adds the feeds you do not have
		yet, and puts feeds in the file's groups.
	</p>
	<div class="buttons">
		<a class="button" href="/api/opml" download>Export to OPML</a>
	</div>
	<form class="import" onsubmit={importOPML}>
		<label class="field">
			<span>OPML file to import</span>
			<input
				type="file"
				accept=".opml,.xml,text/x-opml,text/xml,application/xml"
				onchange={(e) => {
					file = e.currentTarget.files?.[0] ?? null;
					importMessage = null;
				}}
			/>
		</label>
		{#if importMessage}
			<p class="message" class:ok={importMessage.ok} class:error={!importMessage.ok} role="status">
				{importMessage.text}
			</p>
		{/if}
		<div class="buttons">
			<button class="button" disabled={!file || importBusy}>
				{importBusy ? 'Importing…' : 'Import'}
			</button>
		</div>
	</form>
</section>

<section class="panel">
	<h2>Password</h2>
	<p class="hint">
		The password is changed on the server, with <code>fever set-password</code>. That signs every
		device out.
	</p>
</section>

<style>
	h3 {
		margin: 16px 0 6px;
		font-size: 11px;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--text-faint);
	}

	.add {
		margin-bottom: 4px;
	}

	.status {
		flex: none;
		max-width: 50%;
		overflow: hidden;
		text-overflow: ellipsis;
		font-size: 12px;
		font-weight: normal;
		color: var(--text-faint);
	}

	.failing .status {
		color: #a26731;
	}

	.import {
		margin-top: 16px;
	}

	.import input[type='file'] {
		height: auto;
		padding: 6px 0;
		border: 0;
		background: none;
	}

	.panel > .hint:last-child {
		margin-bottom: 0;
	}

	@media (max-width: 699px) {
		.feeds .status {
			display: none;
		}
	}
</style>
