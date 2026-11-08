import { error } from '@sveltejs/kit';
import { loadApi } from '#lib/api/client.ts';
import type { Subscriptions } from '#lib/api/types.ts';
import type { LayoutLoad } from './$types';

export const load: LayoutLoad = async (event) => {
	// Wait for the session check, so a logged-out visit goes straight to the
	// login page instead of also requesting data it cannot get.
	const { unavailable } = await event.parent();
	if (unavailable) error(unavailable.status, unavailable.message);
	event.depends('app:subscriptions');
	return { subscriptions: await loadApi<Subscriptions>(event, '/api/subscriptions') };
};
