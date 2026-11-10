import { loadStream } from '#lib/stream.ts';
import type { PageLoad } from './$types';

// Before anything is searched for, there is no list to load.
export const load: PageLoad = async (event) => {
	const q = event.url.searchParams.get('q')?.trim() ?? '';
	if (!q) return { q, stream: null };
	return { q, ...(await loadStream(event)) };
};
