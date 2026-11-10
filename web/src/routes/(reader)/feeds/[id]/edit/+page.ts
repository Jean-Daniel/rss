import { loadApi } from '#lib/api/client.ts';
import type { FeedSettings } from '#lib/api/types.ts';
import type { PageLoad } from './$types';

export const load: PageLoad = async (event) => ({
	settings: await loadApi<FeedSettings>(event, `/api/feeds/${event.params.id}`)
});
