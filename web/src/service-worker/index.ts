// Caches the app shell so the installed app opens instantly. Without a
// network it still starts and says the server cannot be reached (the API is
// never cached here: read state must come from the server).
import { version } from '$app/env';
import { assets, immutable } from '$app/manifest';
import { self } from '$app/service-worker';

const CACHE = `fever-${version}`;
const SHELL = '/';

const toPath = (p: string) => new URL(p, self.registration.scope).pathname;
const PRECACHE = [
	SHELL,
	...immutable.map((f) => toPath(f.path)),
	...assets.map((f) => toPath(f.path))
];

self.addEventListener('install', (event) => {
	event.waitUntil(caches.open(CACHE).then((cache) => cache.addAll(PRECACHE)));
});

self.addEventListener('activate', (event) => {
	event.waitUntil(
		(async () => {
			for (const key of await caches.keys()) {
				if (key !== CACHE) await caches.delete(key);
			}
			await self.clients.claim();
		})()
	);
});

self.addEventListener('fetch', (event) => {
	const request = event.request;
	if (request.method !== 'GET') return;
	const url = new URL(request.url);
	if (url.origin !== self.location.origin || url.pathname.startsWith('/api/')) return;

	if (request.mode === 'navigate') {
		// Network first, so a new version is picked up; the cached shell
		// when offline (every route is served by the same page).
		event.respondWith(
			fetch(request).catch(async () => (await caches.match(SHELL)) ?? Response.error())
		);
		return;
	}

	event.respondWith((async () => (await caches.match(request)) ?? fetch(request))());
});
