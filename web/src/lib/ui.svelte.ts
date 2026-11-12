import { MediaQuery } from 'svelte/reactivity';

const FEEDS_KEY = 'fever.feedsVisible';

// Hidden unless shown on this device, as in the old Fever setup.
function readFeedsVisible(): boolean {
	try {
		return localStorage.getItem(FEEDS_KEY) === '1';
	} catch {
		return false;
	}
}

/** Layout breakpoints; keep in sync with the media queries in the reader layout. */
export const wide = new MediaQuery('(min-width: 1100px)');
export const phone = new MediaQuery('(max-width: 699px)');
/** A touch screen as the main pointer: no keyboard shortcuts offered. */
export const coarse = new MediaQuery('(pointer: coarse)');

// The window's height without the browser bars that come and go: phones
// change innerHeight as they hide them while scrolling, and sizes taken
// from it would then change under the reader's finger. The root element's
// height is the screen with the bars shown (the small viewport, as 100svh),
// which only changes when the window really does (rotation, resizing).
const steadyHeight = () =>
	typeof document === 'undefined' ? 800 : document.documentElement.clientHeight || 800;

class Viewport {
	/** In CSS pixels. */
	height = $state(steadyHeight());

	constructor() {
		if (typeof window === 'undefined') return;
		// Unchanged while the bars move: the same value does not update
		// anything that depends on it.
		addEventListener('resize', () => (this.height = steadyHeight()), { passive: true });
	}
}

export const viewport = new Viewport();

/** Interface state shared by the reader layout's panels. */
class UI {
	/** Feed column on wide screens; remembered on this device. */
	feedsVisible = $state(readFeedsVisible());
	/** Feed list shown over the items on medium screens. */
	feedsOverlay = $state(false);
	/** Groups and feeds drawer on phones. */
	drawerOpen = $state(false);

	toggleFeeds() {
		if (wide.current) {
			this.feedsVisible = !this.feedsVisible;
			try {
				localStorage.setItem(FEEDS_KEY, this.feedsVisible ? '1' : '0');
			} catch {
				// storage unavailable (private mode): keep it for this visit only
			}
		} else {
			this.feedsOverlay = !this.feedsOverlay;
		}
	}

	closePanels() {
		this.feedsOverlay = false;
		this.drawerOpen = false;
	}
}

export const ui = new UI();
