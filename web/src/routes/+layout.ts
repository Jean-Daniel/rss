import { redirect } from '@sveltejs/kit';
import { api, ApiError } from '#lib/api/client.ts';
import type { Session } from '#lib/api/types.ts';
import { loginURL, safeNext } from '#lib/navigation.ts';
import type { LayoutLoad } from './$types';

// A single-page app: everything renders in the browser, the Go server only
// serves files and the JSON API.
export const ssr = false;
export const prerender = false;

export const load: LayoutLoad = async ({ fetch, url, untrack }) => {
	let session: Session;
	try {
		session = await api<Session>('/api/session', {}, fetch);
	} catch (err) {
		// Offline, or the server is restarting. This load must not throw:
		// SvelteKit would then replace the whole app with its bare static
		// error page. Pages show the problem instead (see +error.svelte).
		if (err instanceof ApiError) {
			const status = err.status >= 400 ? err.status : 503;
			return { session: null, unavailable: { status, message: err.message } };
		}
		throw err;
	}

	// Read the URL without making every navigation re-check the session.
	const { path, onLogin, next } = untrack(() => ({
		path: url.pathname + url.search,
		onLogin: url.pathname === '/login',
		next: url.searchParams.get('next')
	}));
	if (!session.authenticated && !onLogin) redirect(307, loginURL(path));
	if (session.authenticated && onLogin) redirect(307, safeNext(next));
	return { session, unavailable: null };
};
