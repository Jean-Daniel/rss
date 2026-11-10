<script lang="ts">
	import type { Defaults } from '#lib/api/types.ts';
	import { fromChoice, toChoice } from '#lib/manage.ts';

	let {
		showImages = $bindable(),
		oldestFirst = $bindable(),
		showUnreadCount = $bindable(),
		defaults,
		forFeed
	}: {
		showImages: boolean | null;
		oldestFirst: boolean | null;
		showUnreadCount: boolean | null;
		defaults: Defaults;
		/** A feed's options fall back to its group's, then to the preferences. */
		forFeed: boolean;
	} = $props();

	// What "Default" means for a group; for a feed it depends on the group
	// it is opened from.
	const fallback = (text: string) => (forFeed ? 'Default' : `Default (${text})`);
</script>

<fieldset class="options">
	<legend>Display</legend>
	<label class="field">
		<span>Order</span>
		<select
			value={toChoice(oldestFirst)}
			onchange={(e) => (oldestFirst = fromChoice(e.currentTarget.value))}
		>
			<option value="default"
				>{fallback(defaults.oldestFirst ? 'oldest first' : 'newest first')}</option
			>
			<option value="yes">Oldest first</option>
			<option value="no">Newest first</option>
		</select>
	</label>
	<label class="field">
		<span>Articles</span>
		<select
			value={toChoice(showImages)}
			onchange={(e) => (showImages = fromChoice(e.currentTarget.value))}
		>
			<option value="default">{fallback(defaults.showImages ? 'with images' : 'text only')}</option>
			<option value="yes">With images</option>
			<option value="no">Text only</option>
		</select>
	</label>
	<label class="field">
		<span>Unread count in the lists</span>
		<select
			value={toChoice(showUnreadCount)}
			onchange={(e) => (showUnreadCount = fromChoice(e.currentTarget.value))}
		>
			<option value="default">{fallback('shown')}</option>
			<option value="yes">Shown</option>
			<option value="no">Hidden</option>
		</select>
	</label>
	{#if forFeed}
		<p class="hint">
			Default follows the group the feed is opened from, then the reading preferences.
		</p>
	{/if}
</fieldset>
