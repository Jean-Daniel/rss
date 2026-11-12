// Folded items: long items show their beginning only, so scrolling past
// one is short (ItemView). On phones they fold on their own; Enter, or a
// tap on a folded item, folds or unfolds one. What the reader chose is kept
// for the session, also when they go back to a list.
import { SvelteMap } from 'svelte/reactivity';

/** By item: folded (true) or unfolded (false) by the reader. */
export const folds = new SvelteMap<number, boolean>();
