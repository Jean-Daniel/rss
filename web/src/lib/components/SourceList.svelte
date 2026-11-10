<script lang="ts">
	import type { Subscriptions } from '#lib/api/types.ts';
	import type { Selection } from '#lib/selection.ts';

	let { subs, selection }: { subs: Subscriptions; selection: Selection } = $props();

	const current = (on: boolean) => (on ? 'page' : undefined);
</script>

<nav aria-label="Sections and groups">
	<ul class="rows">
		<li>
			<a
				href="/"
				class:read={subs.totalUnread === 0}
				aria-current={current(selection.kind === 'all')}
			>
				<svg class="section-icon" viewBox="0 0 16 16" aria-hidden="true">
					<path d="M2 3h12M2 8h12M2 13h12" />
				</svg>
				<span class="label">All items</span>
				{#if subs.totalUnread > 0}<span class="count">{subs.totalUnread}</span>{/if}
			</a>
		</li>
		{#each subs.groups as group (group.id)}
			<li>
				<a
					href="/groups/{group.id}"
					class="group"
					class:read={group.unread === 0}
					aria-current={current(
						(selection.kind === 'group' && selection.group.id === group.id) ||
							((selection.kind === 'feed' || selection.kind === 'manage') &&
								selection.group?.id === group.id)
					)}
				>
					<span class="label">{group.title}</span>
					{#if group.unread > 0 && group.showUnreadCount !== false}
						<span class="count">{group.unread}</span>
					{/if}
				</a>
			</li>
		{/each}
		<li>
			<a
				href="/saved"
				class:read={subs.savedCount === 0}
				aria-current={current(selection.kind === 'saved')}
			>
				<svg class="section-icon" viewBox="0 0 16 16" aria-hidden="true">
					<path d="M4 2h8v12l-4-3-4 3z" />
				</svg>
				<span class="label">Saved</span>
				{#if subs.savedCount > 0}<span class="count">{subs.savedCount}</span>{/if}
			</a>
		</li>
		<li>
			<a href="/search" class="read" aria-current={current(selection.kind === 'search')}>
				<svg class="section-icon" viewBox="0 0 16 16" aria-hidden="true">
					<circle cx="7" cy="7" r="4.5" /><path d="M10.5 10.5L14 14" />
				</svg>
				<span class="label">Search</span>
			</a>
		</li>
	</ul>
</nav>

<style>
	.section-icon {
		flex: none;
		width: 16px;
		height: 16px;
		fill: none;
		stroke: currentColor;
		stroke-width: 1.6;
		stroke-linejoin: round;
		stroke-linecap: round;
	}

	/* Groups are indented under "All items", as in Fever's Kindling list. */
	.group {
		padding-left: 34px;
	}
</style>
