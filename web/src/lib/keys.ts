// Which shortcut a key press is: the table of Fever's shortcuts, which the
// help shows too, and the rules for reading keys. No browser or Svelte
// state here, so it can be tested on its own (keys.test.ts).

export type Command =
	| 'next'
	| 'previous'
	| 'pageDown'
	| 'pageUp'
	| 'open'
	| 'save'
	| 'visit'
	| 'visitAndRead'
	| 'toggleRead'
	| 'markRead'
	| 'markAllRead'
	| 'undoReads'
	| 'allItems'
	| 'saved'
	| 'search'
	| 'focusSearch'
	| 'feeds'
	| 'refresh'
	| 'refreshShown'
	| 'addFeed'
	| 'addGroup'
	| 'edit'
	| 'settings'
	| 'help'
	| 'escape';

export interface Shortcut {
	/** As the help shows them: "Shift R" is R with Shift. */
	keys: string[];
	what: string;
	/**
	 * What each key does, by name: the key as typed, in lower case, with
	 * "shift+" when Shift changes what it does.
	 */
	commands: Record<string, Command>;
	/** Held down, the key goes on. */
	repeats?: boolean;
}

export const SHORTCUTS: { title: string; shortcuts: Shortcut[] }[] = [
	{
		title: 'Reading',
		shortcuts: [
			{
				keys: ['J', 'K'],
				what: 'Next or previous item',
				commands: { j: 'next', k: 'previous' },
				repeats: true
			},
			{
				keys: ['Space', 'Shift Space'],
				what: 'Next or previous item when in view, else a page down or up',
				commands: { ' ': 'pageDown', 'shift+ ': 'pageUp' },
				repeats: true
			},
			{
				keys: ['O', '→'],
				what: 'Open the current item in a new tab (marks it read)',
				commands: { o: 'open', ArrowRight: 'open' }
			},
			{ keys: ['S'], what: 'Save or unsave the current item', commands: { s: 'save' } },
			{
				keys: ['V', 'Shift V'],
				what: "Visit the current item's site, or visit it and mark its feed as read",
				commands: { v: 'visit', 'shift+v': 'visitAndRead' }
			},
			{ keys: ['U'], what: 'Show or hide read items', commands: { u: 'toggleRead' } },
			{
				keys: ['A', 'Shift A'],
				what: 'Mark the group or feed shown as read, or everything',
				commands: { a: 'markRead', 'shift+a': 'markAllRead' }
			},
			{
				keys: ['Z'],
				what: 'Mark the items just read as unread again',
				commands: { z: 'undoReads' }
			}
		]
	},
	{
		title: 'Going places',
		shortcuts: [
			{
				keys: ['2', '3', '5'],
				what: 'All items, Saved, Search',
				commands: { '2': 'allItems', '3': 'saved', '5': 'search' }
			},
			{ keys: ['/'], what: 'Search', commands: { '/': 'focusSearch' } },
			{ keys: ['F'], what: 'Show or hide the feed list', commands: { f: 'feeds' } },
			{
				keys: ['R', 'Shift R'],
				what: 'Refresh all feeds, or the group or feed shown',
				commands: { r: 'refresh', 'shift+r': 'refreshShown' }
			},
			{
				keys: ['N', 'Shift N'],
				what: 'Add a feed, or a group',
				commands: { n: 'addFeed', 'shift+n': 'addGroup' }
			},
			{ keys: ['I'], what: 'Edit the group or feed shown', commands: { i: 'edit' } },
			{ keys: ['P'], what: 'Settings', commands: { p: 'settings' } },
			{ keys: ['?'], what: 'This list', commands: { '?': 'help' } },
			{
				keys: ['Esc'],
				what: 'Close this list, the menu or the feed list; leave the search field; let the current item go',
				commands: { Escape: 'escape' }
			}
		]
	}
];

const byName = new Map(
	SHORTCUTS.flatMap((section) =>
		section.shortcuts.flatMap((s) =>
			Object.entries(s.commands).map(
				([name, command]) => [name, { command, repeats: !!s.repeats }] as const
			)
		)
	)
);

/** A key press: a KeyboardEvent, as far as the shortcuts go. */
export interface Press {
	key: string;
	code: string;
	shiftKey: boolean;
	ctrlKey: boolean;
	metaKey: boolean;
	altKey: boolean;
	repeat: boolean;
	isComposing: boolean;
	target: unknown;
}

interface Element {
	tagName?: string;
	isContentEditable?: boolean;
	closest?(selector: string): unknown;
}

function typingIn(target: unknown): boolean {
	const el = target as Element | null;
	return (
		!!el && (!!el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName ?? ''))
	);
}

// Space presses buttons (it does not follow links): left to them there.
function pressesButton(target: unknown): boolean {
	const el = target as Element | null;
	return !!el?.closest?.('button, summary, [role="button"]');
}

/**
 * The shortcut a key press is, if any. Keys typed in a field are the
 * field's, except Esc; so are those with Ctrl, Cmd or Alt.
 *
 * Letters are read as typed, whatever the keyboard layout (A is A on AZERTY
 * too). Digits are also read by their key, so AZERTY keyboards need no Shift
 * for them, as in Fever; keypad keys only when they type a digit (with
 * NumLock off they are arrows and Page Down).
 */
export function commandFor(p: Press): Command | null {
	if (p.isComposing || p.ctrlKey || p.metaKey || p.altKey) return null;
	if (p.key === 'Escape') return 'escape';
	if (typingIn(p.target)) return null;
	const typed = p.key.length === 1 ? p.key.toLowerCase() : p.key;
	if (typed === ' ' && pressesButton(p.target)) return null;
	const digit =
		/^Digit([0-9])$/.exec(p.code)?.[1] ??
		(/^Numpad[0-9]$/.test(p.code) && /^[0-9]$/.test(p.key) ? p.key : undefined);
	const found =
		(p.shiftKey && byName.get('shift+' + typed)) ||
		byName.get(typed) ||
		(digit !== undefined ? byName.get(digit) : undefined);
	if (!found || (p.repeat && !found.repeats)) return null;
	return found.command;
}
