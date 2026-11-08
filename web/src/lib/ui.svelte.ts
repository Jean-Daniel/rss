import { MediaQuery } from 'svelte/reactivity';

const FEEDS_KEY = 'fever.feedsVisible';

function readFeedsVisible(): boolean {
	try {
		return localStorage.getItem(FEEDS_KEY) !== '0';
	} catch {
		return true;
	}
}

/** Layout breakpoints; keep in sync with the media queries in the reader layout. */
export const wide = new MediaQuery('(min-width: 1100px)');
export const phone = new MediaQuery('(max-width: 699px)');

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
