import { createContext } from 'svelte';
import { invalidate } from '$app/navigation';
import { api, loadApi, type LoadContext } from './api/client.ts';
import type { Subscriptions } from './api/types.ts';

// Subscriptions that reloadSubscriptions fetched, for the load function it
// then runs again.
let fetched: Subscriptions | undefined;

/** The subscriptions, for the reader's load function. */
export async function loadSubscriptions(event: LoadContext): Promise<Subscriptions> {
	return fetched ?? (await loadApi<Subscriptions>(event, '/api/subscriptions'));
}

/**
 * Fetches the subscriptions again and shows them. A failure is thrown as an
 * ApiError and changes nothing on screen: a load function that fails shows
 * the error page instead of the reader, so the request is made here, and the
 * load function only runs again once there is an answer for it.
 */
export async function reloadSubscriptions(): Promise<void> {
	const subs = await api<Subscriptions>('/api/subscriptions');
	fetched = subs;
	try {
		await invalidate('app:subscriptions');
	} finally {
		// Unless a later reload has put its own answer there meanwhile.
		if (fetched === subs) fetched = undefined;
	}
}

/**
 * The subscriptions as the reader shows them, with the unread counts adjusted
 * for what was read here (withLocalCounts). The reader's layout works them
 * out once, for itself and the components below it.
 */
export const [shownSubscriptions, setShownSubscriptions] = createContext<() => Subscriptions>();
