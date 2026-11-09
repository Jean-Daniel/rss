import { error } from '@sveltejs/kit';
import { loadApi } from './api/client.ts';
import type { ItemPage, Subscriptions } from './api/types.ts';
import { showReadPref } from './prefs.ts';
import { contextGroup, selectionFrom, type Selection } from './selection.ts';

/** Which items a list shows. */
export type Scope = { kind: 'all' } | { kind: 'saved' } | { kind: 'group' | 'feed'; id: number };

/** How a list is shown: the feed's options, else its group's, else the defaults. */
export interface StreamOptions {
	unreadOnly: boolean;
	oldestFirst: boolean;
	showImages: boolean;
}

export function scopeOf(sel: Selection): Scope | null {
	switch (sel.kind) {
		case 'all':
		case 'saved':
			return { kind: sel.kind };
		case 'group':
			return { kind: 'group', id: sel.group.id };
		case 'feed':
			return { kind: 'feed', id: sel.feed.id };
		case 'missing':
			return null;
	}
}

export function optionsFor(sel: Selection, subs: Subscriptions, showRead: boolean): StreamOptions {
	const d = subs.defaults;
	if (sel.kind === 'saved') {
		// Saved items: all of them, most recent first.
		return { unreadOnly: false, oldestFirst: false, showImages: d.showImages };
	}
	const group = contextGroup(sel);
	const feed = sel.kind === 'feed' ? sel.feed : null;
	return {
		unreadOnly: !showRead,
		oldestFirst: feed?.oldestFirst ?? group?.oldestFirst ?? d.oldestFirst,
		showImages: feed?.showImages ?? group?.showImages ?? d.showImages
	};
}

export function itemsURL(scope: Scope, opts: StreamOptions, after?: string | null): string {
	const q = new URLSearchParams({
		scope: scope.kind,
		order: opts.oldestFirst ? 'oldest' : 'newest'
	});
	if (scope.kind === 'group' || scope.kind === 'feed') q.set('id', String(scope.id));
	if (opts.unreadOnly) q.set('unread', '1');
	if (after) q.set('after', after);
	return `/api/items?${q}`;
}

export interface StreamData {
	scope: Scope;
	options: StreamOptions;
	page: ItemPage;
}

interface StreamLoadEvent {
	fetch: typeof fetch;
	url: URL;
	params: Record<string, string>;
	route: { id: string | null };
	parent(): Promise<Record<string, unknown>>;
	depends(...deps: `${string}:${string}`[]): void;
	untrack<T>(fn: () => T): T;
}

/**
 * The load function of the item list pages. The subscriptions are read
 * without depending on them: they reload every few seconds while items are
 * marked read, and that must not reset the list.
 */
export async function loadStream(event: StreamLoadEvent): Promise<{ stream: StreamData }> {
	const { subscriptions } = (await event.untrack(() => event.parent())) as {
		subscriptions: Subscriptions;
	};
	event.depends('app:stream');
	const sel = selectionFrom(event.route.id, event.params, event.url.searchParams, subscriptions);
	const scope = scopeOf(sel);
	if (!scope) error(404, 'This group or feed does not exist any more.');
	const options = optionsFor(sel, subscriptions, showReadPref(subscriptions.defaults.showRead));
	const page = await loadApi<ItemPage>(event, itemsURL(scope, options));
	return { stream: { scope, options, page } };
}
