import type { Feed } from './api/types.ts';
import { formatDate } from './time.ts';

/** Why a feed is marked stale, for tooltips and screen readers. */
export function staleText(feed: Feed, retentionWeeks: number): string {
	return feed.lastUpdatedAt
		? `No new items since ${formatDate(feed.lastUpdatedAt)}`
		: `No new items in the last ${retentionWeeks} weeks`;
}
