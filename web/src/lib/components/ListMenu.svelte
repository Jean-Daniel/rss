<script lang="ts">
	import { afterNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import { shortcuts } from '#lib/shortcuts.svelte.ts';
	import type { StreamData } from '#lib/stream.ts';
	import { coarse, phone, ui, wide } from '#lib/ui.svelte.ts';

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
	const showFeeds = $derived(!phone.current);
	const showHelp = $derived(!coarse.current);
	// No button for an empty menu (on a phone, outside a group or feed).
	const any = $derived(reading || !!edit || showFeeds || showHelp);

	afterNavigate(() => (open = false));

	function run(act: () => void) {
		open = false;
		act();
	}

	// Closed by a click elsewhere, Esc (before the reader's own Esc), or
	// the focus leaving it (onFocusOut).
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

	$effect(() => {
		if (!any) open = false;
	});

	function focusFirst(menu: HTMLElement) {
		menu.querySelector<HTMLElement>('[role="menuitem"]')?.focus();
	}

	// Arrow keys, Home and End move between the items, as in any menu.
	const moves: Record<string, (at: number, n: number) => number> = {
		ArrowDown: (at, n) => (at + 1) % n,
		ArrowUp: (at, n) => (at - 1 + n) % n,
		Home: () => 0,
		End: (_, n) => n - 1
	};
	function onMenuKey(e: KeyboardEvent) {
		const move = moves[e.key];
		if (!move) return;
		const menu = e.currentTarget as HTMLElement;
		const items = [...menu.querySelectorAll<HTMLElement>('[role="menuitem"]')];
		if (items.length === 0) return;
		e.preventDefault();
		e.stopPropagation(); // not the reader's keys, nor the first-scroll outline
		items[move(items.indexOf(document.activeElement as HTMLElement), items.length)]?.focus();
	}

	// Tab out of it. Not a click (no related target in Safari, which does
	// not focus buttons): the click is seen above, and closing here would
	// lose a click on an item.
	function onFocusOut(e: FocusEvent) {
		if (open && e.relatedTarget instanceof Node && !root?.contains(e.relatedTarget)) open = false;
	}
</script>

{#if any}
	<div class="list-menu" bind:this={root} onfocusout={onFocusOut}>
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
			<div
				class="menu"
				role="menu"
				aria-label="List actions"
				tabindex="-1"
				{@attach focusFirst}
				onkeydown={onMenuKey}
			>
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
				{#if showFeeds}
					<button role="menuitem" onclick={() => run(() => ui.toggleFeeds())}>
						{feedsShown ? 'Hide the feed list' : 'Show the feed list'}
					</button>
				{/if}
				{#if showHelp}
					<button role="menuitem" onclick={() => run(() => (shortcuts.helpOpen = true))}>
						Keyboard shortcuts
					</button>
				{/if}
			</div>
		{/if}
	</div>
{/if}

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
