/** "just now", "5 min ago", "3 h ago", "yesterday", "4 days ago". */
export function ago(iso: string, now: number): string {
	const seconds = Math.max(0, Math.round((now - Date.parse(iso)) / 1000));
	if (seconds < 60) return 'just now';
	const minutes = Math.round(seconds / 60);
	if (minutes < 60) return `${minutes} min ago`;
	const hours = Math.round(minutes / 60);
	if (hours < 24) return `${hours} h ago`;
	const days = Math.round(hours / 24);
	return days === 1 ? 'yesterday' : `${days} days ago`;
}

/** A short date in the reader's language, e.g. "3 Sep 2026". */
export function formatDate(iso: string): string {
	return new Date(iso).toLocaleDateString(undefined, {
		day: 'numeric',
		month: 'short',
		year: 'numeric'
	});
}
