import type { Feed, Group, Subscriptions } from './api/types.ts';

/** What the reader is showing, derived from the URL. */
export type Selection =
	| { kind: 'all' }
	| { kind: 'saved' }
	// q is empty before searching.
	| { kind: 'search'; q: string }
	| { kind: 'group'; group: Group }
	| { kind: 'feed'; feed: Feed; group: Group | null }
	// Settings, and the pages that add or change a feed or a group.
	| { kind: 'manage'; title: string; feed: Feed | null; group: Group | null }
	| { kind: 'missing' };

export function selectionFrom(
	routeId: string | null,
	params: Record<string, string>,
	search: { get(name: string): string | null },
	subs: Subscriptions
): Selection {
	const groupParam = () => subs.groups.find((g) => g.id === Number(search.get('group'))) ?? null;
	switch (routeId) {
		case '/(reader)':
			return { kind: 'all' };
		case '/(reader)/saved':
			return { kind: 'saved' };
		case '/(reader)/search':
			return { kind: 'search', q: search.get('q')?.trim() ?? '' };
		case '/(reader)/groups/[id]': {
			const group = subs.groups.find((g) => g.id === Number(params.id));
			return group ? { kind: 'group', group } : { kind: 'missing' };
		}
		case '/(reader)/feeds/[id]': {
			const feed = subs.feeds.find((f) => f.id === Number(params.id));
			if (!feed) return { kind: 'missing' };
			// ?group= keeps the feed list on the group the feed was opened from.
			return { kind: 'feed', feed, group: groupParam() };
		}
		case '/(reader)/settings':
			return { kind: 'manage', title: 'Settings', feed: null, group: null };
		case '/(reader)/feeds/new':
			return { kind: 'manage', title: 'Add a feed', feed: null, group: null };
		case '/(reader)/groups/new':
			return { kind: 'manage', title: 'Add a group', feed: null, group: null };
		case '/(reader)/feeds/[id]/edit': {
			const feed = subs.feeds.find((f) => f.id === Number(params.id));
			return feed
				? { kind: 'manage', title: feed.title, feed, group: groupParam() }
				: { kind: 'missing' };
		}
		case '/(reader)/groups/[id]/edit': {
			const group = subs.groups.find((g) => g.id === Number(params.id));
			return group
				? { kind: 'manage', title: group.title, feed: null, group }
				: { kind: 'missing' };
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
		case 'search':
			return 'Search';
		case 'group':
			return sel.group.title;
		case 'feed':
			return sel.feed.title;
		case 'manage':
			return sel.title;
		case 'missing':
			return 'Not found';
	}
}

/** The group whose feeds are listed next to the items, if any. */
export function contextGroup(sel: Selection): Group | null {
	if (sel.kind === 'group' || sel.kind === 'feed' || sel.kind === 'manage') return sel.group;
	return null;
}

/** The settings page of the group or feed shown, if any. */
export function editHref(sel: Selection): string | null {
	if (sel.kind === 'group') return `/groups/${sel.group.id}/edit`;
	if (sel.kind === 'feed') {
		return sel.group
			? `/feeds/${sel.feed.id}/edit?group=${sel.group.id}`
			: `/feeds/${sel.feed.id}/edit`;
	}
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
