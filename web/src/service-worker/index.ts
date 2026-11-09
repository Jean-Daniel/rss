// Caches the app shell so the installed app starts from the device. When the
// server is down or the network does not answer, it still starts and says the
// server cannot be reached (the API is never cached here: read state must
// come from the server).
//
// A new version takes over as soon as it is installed: an installed app is
// rarely fully closed, so waiting for that would keep it on the old version
// indefinitely. The page shows a "new version" banner (UpdateBanner).
import { version } from '$app/env';
import { assets, immutable } from '$app/manifest';
import { self } from '$app/service-worker';

const PREFIX = 'fever-';
const CACHE = PREFIX + version;
const SHELL = '/';
/** How long a page load waits for the server before using the cached shell. */
const NETWORK_TIMEOUT = 3000;

const toPath = (p: string) => new URL(p, self.registration.scope).pathname;
const PRECACHE = [
	SHELL,
	...immutable.map((f) => toPath(f.path)),
	...assets.map((f) => toPath(f.path))
];

self.addEventListener('install', (event) => {
	event.waitUntil(
		(async () => {
			const cache = await caches.open(CACHE);
			try {
				await cache.addAll(PRECACHE);
			} catch (err) {
				// Leave no partial cache behind: activate keeps the newest one.
				await caches.delete(CACHE);
				throw err;
			}
			await self.skipWaiting();
		})()
	);
});

self.addEventListener('activate', (event) => {
	event.waitUntil(
		(async () => {
			// Keep the previous version too: a page still running it may load
			// more of its files (the server only has the new ones). keys()
			// lists caches in the order they were created, and only complete
			// installs leave one, so the last other cache is the previous
			// version.
			const older = (await caches.keys()).filter((key) => key.startsWith(PREFIX) && key !== CACHE);
			for (const key of older.slice(0, -1)) await caches.delete(key);
			await self.clients.claim();
		})()
	);
});

/** This version's copy first; the previous version's only for its own files. */
async function cached(request: Request | string): Promise<Response | undefined> {
	return (await (await caches.open(CACHE)).match(request)) ?? (await caches.match(request));
}

/**
 * Page loads go to the network first, so a new version is picked up. The
 * cached shell (every route is served by the same page) is used when the
 * network fails, is slower than NETWORK_TIMEOUT, or a proxy answers with an
 * error page because the server is down.
 */
async function navigate(request: Request): Promise<Response> {
	const network = fetch(request);
	const timeout = new Promise<'timeout'>((resolve) =>
		setTimeout(() => resolve('timeout'), NETWORK_TIMEOUT)
	);
	const first = await Promise.race([network.catch(() => null), timeout]);
	if (first instanceof Response && first.status < 500) return first;
	const shell = await cached(SHELL);
	if (shell) return shell;
	// Nothing cached (the worker is still installing): the network's answer.
	if (first instanceof Response) return first;
	return network.catch(() => Response.error());
}

self.addEventListener('fetch', (event) => {
	const request = event.request;
	if (request.method !== 'GET') return;
	const url = new URL(request.url);
	if (url.origin !== self.location.origin || url.pathname.startsWith('/api/')) return;

	if (request.mode === 'navigate') {
		event.respondWith(navigate(request));
		return;
	}

	// Files not cached here, in practice SvelteKit's new-version check
	// (/_app/version.json), give up after NETWORK_TIMEOUT: SvelteKit makes
	// that check before showing an error page, so a server that does not
	// answer would otherwise leave the page blank.
	event.respondWith(
		(async () =>
			(await cached(request)) ?? fetch(request, { signal: AbortSignal.timeout(NETWORK_TIMEOUT) }))()
	);
});
