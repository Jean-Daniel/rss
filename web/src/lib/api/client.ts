import { error, redirect } from '@sveltejs/kit';
import { loginURL } from '#lib/navigation.ts';
import type { ErrorResponse } from './types.ts';

export class ApiError extends Error {
	constructor(
		/** HTTP status, or 0 when the server could not be reached. */
		public status: number,
		message: string
	) {
		super(message);
	}
}

export const UNREACHABLE = 'Cannot reach the server. Check your connection and try again.';

/**
 * Calls the Go API and returns the parsed JSON. Failures, including network
 * errors, are thrown as ApiError; callers decide what to do with them. In
 * `load` functions, use loadApi instead.
 */
export async function api<T>(
	path: string,
	init: RequestInit = {},
	fetchFn: typeof fetch = fetch
): Promise<T> {
	const headers = new Headers(init.headers);
	if (init.body !== undefined && !headers.has('Content-Type')) {
		headers.set('Content-Type', 'application/json');
	}
	let res: Response;
	try {
		res = await fetchFn(path, { ...init, headers, credentials: 'same-origin' });
	} catch (err) {
		if (err instanceof DOMException && err.name === 'AbortError') throw err;
		throw new ApiError(0, UNREACHABLE);
	}
	if (!res.ok) {
		let message = res.statusText || `HTTP ${res.status}`;
		try {
			message = ((await res.json()) as ErrorResponse).error || message;
		} catch {
			// Not the server's JSON: a proxy's error page. 502-504 from the
			// proxy (nginx) mean the server itself is down or not answering.
			if (res.status >= 502 && res.status <= 504) throw new ApiError(0, UNREACHABLE);
		}
		throw new ApiError(res.status, message);
	}
	// 202/204 and other answers without a JSON body carry no data.
	if (!res.headers.get('Content-Type')?.startsWith('application/json')) return undefined as T;
	return (await res.json()) as T;
}

/** The parts of a load event loadApi needs. */
export interface LoadContext {
	fetch: typeof fetch;
	url: URL;
	untrack<T>(fn: () => T): T;
}

/**
 * api() for `load` functions: an ended session redirects to the login page
 * (coming back here afterwards), other failures become SvelteKit errors shown
 * by +error.svelte.
 */
export async function loadApi<T>(event: LoadContext, path: string): Promise<T> {
	try {
		return await api<T>(path, {}, event.fetch);
	} catch (err) {
		if (!(err instanceof ApiError)) throw err;
		if (err.status === 401) {
			redirect(307, loginURL(event.untrack(() => event.url.pathname + event.url.search)));
		}
		error(err.status >= 400 ? err.status : 503, err.message);
	}
}
