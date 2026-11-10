// Fever's keyboard shortcuts (the keys: keys.ts). The reader's layout
// listens for keys; the item list on screen (Stream) does the ones that act
// on items.
import { commandFor, type Command } from './keys.ts';

/** An item marked unread again by Z. */
export interface Restored {
	id: number;
	feedId: number;
}

/** What the item list on screen does for the keys that act on items. */
export interface ListKeys {
	/** J and K: the next or previous item to the top of the reading area. */
	next(): void;
	previous(): void;
	/** Space and Shift+Space: to the next (previous) item if it is in view, else a page. */
	page(back: boolean): void;
	/** O: the current item, in a new tab (marked read). */
	open(): void;
	/** S */
	toggleSaved(): void;
	/** V: the current item's site, in a new tab; with markRead, its feed is marked read. */
	visitSite(markRead: boolean): void;
	/** A: this list as read; Shift+A: everything. */
	markAllRead(everything: boolean): void;
	/** U: Unread or All. */
	toggleShowRead(): void;
	/** After Z: these items are unread again. */
	unreadAgain(items: Restored[]): void;
}

/** What the reader's layout does for the other keys. */
export interface AppKeys {
	go(path: string): void;
	/** R: every feed; Shift+R: the group or feed shown. */
	refresh(shown: boolean): void;
	toggleFeeds(): void;
	/** I */
	edit(): void;
	/** /: the search field. */
	search(): void;
	/** Z */
	undoReads(): void;
	/** Esc: the drawer or the feed list; false when nothing was open. */
	closePanels(): boolean;
}

class Shortcuts {
	/** The list of shortcuts is shown. */
	helpOpen = $state(false);
	/** The last items were reached with keys: the current item is marked. */
	keyboard = $state(false);
	list: ListKeys | null = null;

	/** Registers the list on screen; returns the function that removes it. */
	setList(list: ListKeys): () => void {
		this.list = list;
		return () => {
			if (this.list === list) this.list = null;
		};
	}
}

export const shortcuts = new Shortcuts();

/** Does a key press; returns whether it was a shortcut. */
export function handleKey(e: KeyboardEvent, app: AppKeys): boolean {
	if (e.defaultPrevented) return false;
	const command = commandFor(e);
	if (!command) return false;
	if (command === 'escape') {
		if (shortcuts.helpOpen) {
			shortcuts.helpOpen = false;
			return true;
		}
		if (e.target instanceof HTMLElement && e.target.matches('input, textarea, select')) {
			e.target.blur();
			return true;
		}
		return app.closePanels();
	}
	if (shortcuts.helpOpen) return false;
	return run[command](shortcuts.list, app);
}

type Run = (list: ListKeys | null, app: AppKeys) => boolean;

// Keys for the list on screen: nothing to do without one.
const onList =
	(act: (list: ListKeys) => void): Run =>
	(list) => {
		if (!list) return false;
		shortcuts.keyboard = true;
		act(list);
		return true;
	};
const onApp =
	(act: (app: AppKeys) => void): Run =>
	(_, app) => {
		act(app);
		return true;
	};

// Every command, so none is listed in the help without doing anything.
const run: Record<Exclude<Command, 'escape'>, Run> = {
	next: onList((l) => l.next()),
	previous: onList((l) => l.previous()),
	pageDown: onList((l) => l.page(false)),
	pageUp: onList((l) => l.page(true)),
	open: onList((l) => l.open()),
	save: onList((l) => l.toggleSaved()),
	visit: onList((l) => l.visitSite(false)),
	visitAndRead: onList((l) => l.visitSite(true)),
	toggleRead: onList((l) => l.toggleShowRead()),
	markRead: onList((l) => l.markAllRead(false)),
	markAllRead: onList((l) => l.markAllRead(true)),
	undoReads: onApp((a) => a.undoReads()),
	allItems: onApp((a) => a.go('/')),
	saved: onApp((a) => a.go('/saved')),
	search: onApp((a) => a.go('/search')),
	focusSearch: onApp((a) => a.search()),
	feeds: onApp((a) => a.toggleFeeds()),
	refresh: onApp((a) => a.refresh(false)),
	refreshShown: onApp((a) => a.refresh(true)),
	addFeed: onApp((a) => a.go('/feeds/new')),
	addGroup: onApp((a) => a.go('/groups/new')),
	edit: onApp((a) => a.edit()),
	settings: onApp((a) => a.go('/settings')),
	help: () => {
		shortcuts.helpOpen = true;
		return true;
	}
};
