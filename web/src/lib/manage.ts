import { goto } from '$app/navigation';
import { ApiError } from './api/client.ts';
import { itemState } from './item-state.svelte.ts';
import { toLogin } from './navigation.ts';

/**
 * After a change was saved: the subscriptions are reloaded, so the reader
 * shows it, then the reader goes to `to`.
 */
export async function afterSave(to?: string): Promise<void> {
	try {
		await itemState.reloadCounts();
	} catch {
		// The change is saved. The sidebar catches up on its next reload.
	}
	if (to) await goto(to);
}

/** After a delete: away from the page of what is gone, then the reload. */
export async function afterDelete(to: string): Promise<void> {
	await goto(to);
	await itemState.reloadCountsQuietly();
}

/** The message for a failed request. If the session ended, the login page opens. */
export function failure(err: unknown): string {
	if (err instanceof ApiError && err.status === 401) {
		void toLogin();
		return 'You have been logged out.';
	}
	return err instanceof Error ? err.message : String(err);
}

/** A tri-state option (null: the next level's) as a <select> value, and back. */
export type Choice = 'default' | 'yes' | 'no';

export function toChoice(v: boolean | null): Choice {
	return v === null ? 'default' : v ? 'yes' : 'no';
}

export function fromChoice(c: string): boolean | null {
	return c === 'default' ? null : c === 'yes';
}
