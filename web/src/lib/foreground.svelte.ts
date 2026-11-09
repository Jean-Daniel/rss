// When to catch up with changes made elsewhere (refreshed feeds, reads on
// another device), and a clock for "5 min ago" labels. One listener and one
// timer serve the whole app.

const RESUME_AFTER = 30_000; // back in the foreground after at least this long
const EVERY = 5 * 60_000; // and this often while the app stays visible
const TICK = 30_000;

class Clock {
	/** The current time, updated every 30 seconds. */
	now = $state(Date.now());
}

export const clock = new Clock();

const callbacks = new Set<() => void>();
let lastRun = Date.now();
let started = false;

function runAll() {
	lastRun = Date.now();
	for (const fn of callbacks) fn();
}

function start() {
	if (started || typeof document === 'undefined') return;
	started = true;
	document.addEventListener('visibilitychange', () => {
		clock.now = Date.now();
		if (!document.hidden && Date.now() - lastRun >= RESUME_AFTER) runAll();
	});
	setInterval(() => {
		clock.now = Date.now();
		if (!document.hidden && Date.now() - lastRun >= EVERY) runAll();
	}, TICK);
}

/**
 * Calls fn when the app comes back to the foreground after 30 seconds or
 * more, and every 5 minutes while it is visible. Returns a function that
 * stops it (suitable as an $effect's cleanup).
 */
export function onResume(fn: () => void): () => void {
	start();
	callbacks.add(fn);
	return () => {
		callbacks.delete(fn);
	};
}
