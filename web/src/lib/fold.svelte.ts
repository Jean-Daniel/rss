// Folded items: long items show their beginning only, so scrolling past
// one is short (ItemView). On phones they fold on their own; Enter, or a
// tap on a folded item, folds or unfolds one. What the reader chose is kept
// for the session, also when they go back to a list.
import { SvelteMap } from 'svelte/reactivity';

/** By item: folded (true) or unfolded (false) by the reader. */
export const folds = new SvelteMap<number, boolean>();

/** The items on screen long enough to fold: whether each is folded now. */
const foldable = new Map<number, () => boolean>();

/** Called by an item long enough to fold; returns the function that removes it. */
export function registerFoldable(id: number, folded: () => boolean): () => void {
	foldable.set(id, folded);
	return () => {
		if (foldable.get(id) === folded) foldable.delete(id);
	};
}

/** Folds or unfolds an item; returns whether it is now folded, or null if it cannot fold. */
export function toggleFold(id: number): boolean | null {
	const folded = foldable.get(id);
	if (!folded) return null;
	const fold = !folded();
	folds.set(id, fold);
	return fold;
}
