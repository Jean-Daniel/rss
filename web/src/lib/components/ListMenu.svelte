<script lang="ts">
	import { afterNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import { shortcuts } from '#lib/shortcuts.svelte.ts';
	import type { StreamData } from '#lib/stream.ts';
	import { phone, ui, wide } from '#lib/ui.svelte.ts';

	// The actions of the list shown, in a menu: they used to take a row of
	// buttons above the list.
	let {
		edit,
		editLabel,
		unread
	}: {
		/** The settings page of the group or feed shown, if any. */
		edit: string | null;
		editLabel: string;
		unread: number;
	} = $props();

	let open = $state(false);
	let root: HTMLElement | undefined = $state();
	let button: HTMLButtonElement | undefined = $state();

	const stream = $derived(page.data.stream as StreamData | null | undefined);
	// Saved items and search results have neither Unread/All nor "mark all
	// as read" (see Stream).
	const reading = $derived(
		!!stream && stream.scope.kind !== 'saved' && stream.scope.kind !== 'search'
	);
	const feedsShown = $derived(wide.current ? ui.feedsVisible : ui.feedsOverlay);
	const touch = typeof matchMedia === 'function' && matchMedia('(pointer: coarse)').matches;

	afterNavigate(() => (open = false));

	function run(act: () => void) {
		open = false;
		act();
	}

	// Closed by a click elsewhere or Esc (before the reader's own Esc).
	$effect(() => {
		if (!open) return;
		const outside = (e: PointerEvent) => {
			if (!root?.contains(e.target as Node)) open = false;
		};
		const esc = (e: KeyboardEvent) => {
			if (e.key !== 'Escape') return;
			e.preventDefault();
			e.stopPropagation();
			open = false;
			button?.focus();
		};
		addEventListener('pointerdown', outside, true);
		addEventListener('keydown', esc, true);
		return () => {
			removeEventListener('pointerdown', outside, true);
			removeEventListener('keydown', esc, true);
		};
	});

	function focusFirst(menu: HTMLElement) {
		menu.querySelector<HTMLElement>('[role="menuitem"]')?.focus();
	}
</script>

<div class="list-menu" bind:this={root}>
	<button
		bind:this={button}
		class="icon-button"
		aria-haspopup="menu"
		aria-expanded={open}
		aria-label="List actions"
		title="List actions"
		onclick={() => (open = !open)}
	>
		<svg viewBox="0 0 18 18" aria-hidden="true">
			<circle cx="4" cy="9" r="1.3" /><circle cx="9" cy="9" r="1.3" /><circle
				cx="14"
				cy="9"
				r="1.3"
			/>
		</svg>
	</button>
	{#if open}
		<div class="menu" role="menu" aria-label="List actions" {@attach focusFirst}>
			{#if reading && stream}
				<button role="menuitem" onclick={() => run(() => shortcuts.list?.toggleShowRead())}>
					{stream.options.unreadOnly ? 'Show read items' : 'Hide read items'}
				</button>
				{#if unread > 0}
					<button role="menuitem" onclick={() => run(() => shortcuts.list?.markAllRead(false))}>
						Mark all as read
					</button>
				{/if}
			{/if}
			{#if edit}
				<a role="menuitem" href={edit} onclick={() => (open = false)}>{editLabel}</a>
			{/if}
			{#if !phone.current}
				<button role="menuitem" onclick={() => run(() => ui.toggleFeeds())}>
					{feedsShown ? 'Hide the feed list' : 'Show the feed list'}
				</button>
			{/if}
			{#if !touch}
				<button role="menuitem" onclick={() => run(() => (shortcuts.helpOpen = true))}>
					Keyboard shortcuts
				</button>
			{/if}
		</div>
	{/if}
</div>

<style>
	.list-menu {
		position: relative;
		flex: none;
	}

	.icon-button[aria-expanded='true'] {
		background: var(--row-selected);
		color: var(--text);
	}

	svg {
		fill: currentColor;
		stroke: none;
	}

	.menu {
		position: absolute;
		top: calc(100% + 4px);
		right: 0;
		z-index: 30;
		display: grid;
		min-width: 210px;
		padding: 4px;
		border-radius: var(--radius-card);
		background: var(--surface);
		box-shadow: var(--shadow-float);
	}

	.menu > * {
		display: flex;
		align-items: center;
		min-height: 34px;
		padding: 0 12px;
		border: 0;
		border-radius: var(--radius);
		background: transparent;
		color: var(--text);
		font: inherit;
		font-weight: normal;
		text-align: left;
		white-space: nowrap;
		cursor: pointer;
	}

	.menu > *:hover,
	.menu > *:focus-visible {
		background: var(--row-hover);
		color: var(--text);
		outline: none;
	}

	@media (pointer: coarse) {
		.menu > * {
			min-height: 44px;
		}
	}
</style>
