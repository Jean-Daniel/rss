<script lang="ts">
	import { goto, invalidate } from '$app/navigation';
	import Stream from '#lib/components/Stream.svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	// Ready to type in when the page opens empty.
	function focusWhenEmpty(node: HTMLInputElement) {
		if (!data.q) node.focus();
	}

	// To /search?q=…, as the form would go; the same words again search
	// again (a navigation to the same address would not).
	async function search(event: SubmitEvent) {
		event.preventDefault();
		const form = event.currentTarget as HTMLFormElement;
		const q = String(new FormData(form).get('q') ?? '').trim();
		if (q !== '' && q === data.q) {
			await invalidate('app:stream');
		} else {
			await goto(q ? `/search?${new URLSearchParams({ q })}` : '/search');
		}
	}
</script>

<form class="search" role="search" action="/search" onsubmit={search}>
	<input
		class="input"
		type="search"
		name="q"
		value={data.q}
		maxlength="200"
		placeholder="Words to find"
		aria-label="Words to find"
		autocomplete="off"
		enterkeyhint="search"
		{@attach focusWhenEmpty}
	/>
	<button class="button">Search</button>
</form>

{#if data.stream}
	{#key data.stream}
		<Stream stream={data.stream} subs={data.subscriptions} />
	{/key}
{:else}
	<p class="about">
		Finds the items whose title, author or text has every word given, or a word starting with it,
		whatever the case and accents. It searches the items of the last
		{data.subscriptions.retentionWeeks} weeks, and saved items.
	</p>
{/if}

<style>
	.search {
		display: flex;
		gap: 8px;
		margin-bottom: 14px;
	}

	.search input {
		flex: 1;
		min-width: 0;
	}

	.about {
		margin: 0;
		color: var(--text-muted);
	}
</style>
