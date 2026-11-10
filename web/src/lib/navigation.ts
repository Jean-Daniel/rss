import { goto } from '$app/navigation';

/** The login page, remembering where to go back to afterwards. */
export function loginURL(next: string): string {
	return next && next !== '/' && next !== '/login'
		? `/login?next=${encodeURIComponent(next)}`
		: '/login';
}

/**
 * The session has ended while the reader was open: to the login page, and
 * back to this page afterwards.
 */
export async function toLogin(): Promise<void> {
	if (location.pathname === '/login') return;
	await goto(loginURL(location.pathname + location.search), { refreshAll: true });
}

/**
 * Where to go after logging in. The value is resolved the way the browser
 * would resolve it, and only addresses inside the app are accepted, so a
 * crafted link cannot send the user to another site.
 */
export function safeNext(next: string | null): string {
	if (!next) return '/';
	let url: URL;
	try {
		url = new URL(next, location.origin);
	} catch {
		return '/';
	}
	if (url.origin !== location.origin || url.pathname.startsWith('/login')) return '/';
	return url.pathname + url.search + url.hash;
}
