import type { Feed, Group, Subscriptions } from './api/types.ts';

/** What the reader is showing, derived from the URL. */
export type Selection =
	| { kind: 'all' }
	| { kind: 'saved' }
	| { kind: 'group'; group: Group }
	| { kind: 'feed'; feed: Feed; group: Group | null }
	| { kind: 'missing' };

export function selectionFrom(
	routeId: string | null,
	params: Record<string, string>,
	search: { get(name: string): string | null },
	subs: Subscriptions
): Selection {
	switch (routeId) {
		case '/(reader)':
			return { kind: 'all' };
		case '/(reader)/saved':
			return { kind: 'saved' };
		case '/(reader)/groups/[id]': {
			const group = subs.groups.find((g) => g.id === Number(params.id));
			return group ? { kind: 'group', group } : { kind: 'missing' };
		}
		case '/(reader)/feeds/[id]': {
			const feed = subs.feeds.find((f) => f.id === Number(params.id));
			if (!feed) return { kind: 'missing' };
			// ?group= keeps the feed list on the group the feed was opened from.
			const group = subs.groups.find((g) => g.id === Number(search.get('group'))) ?? null;
			return { kind: 'feed', feed, group };
		}
		default:
			return { kind: 'missing' };
	}
}

export function selectionTitle(sel: Selection): string {
	switch (sel.kind) {
		case 'all':
			return 'All items';
		case 'saved':
			return 'Saved';
		case 'group':
			return sel.group.title;
		case 'feed':
			return sel.feed.title;
		case 'missing':
			return 'Not found';
	}
}

/** The group whose feeds are listed next to the items, if any. */
export function contextGroup(sel: Selection): Group | null {
	if (sel.kind === 'group') return sel.group;
	if (sel.kind === 'feed') return sel.group;
	return null;
}

export function unreadOf(sel: Selection, subs: Subscriptions): number {
	switch (sel.kind) {
		case 'all':
			return subs.totalUnread;
		case 'group':
			return sel.group.unread;
		case 'feed':
			return sel.feed.unread;
		default:
			return 0;
	}
}
