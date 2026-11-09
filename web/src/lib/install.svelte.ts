// "Install app" for browsers that offer it to the page (Chrome and Edge on
// desktop and Android). Safari has no such API: there the app is added with
// Share → Add to Home Screen (iOS) or File → Add to Dock (macOS).

interface BeforeInstallPromptEvent extends Event {
	prompt(): Promise<void>;
	readonly userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>;
}

class Install {
	#event = $state.raw<BeforeInstallPromptEvent | null>(null);

	/** The browser would install the app if asked. */
	get available(): boolean {
		return this.#event !== null;
	}

	/** Shows the browser's install dialog. */
	async prompt() {
		const event = this.#event;
		if (!event) return;
		// A prompt can be shown only once; the browser fires a new event if
		// the app can still be installed after a dismissal.
		this.#event = null;
		await event.prompt();
	}

	constructor() {
		if (typeof window === 'undefined') return;
		window.addEventListener('beforeinstallprompt', (event) => {
			// Keep the browser's own mini-infobar from covering the reader.
			event.preventDefault();
			this.#event = event as BeforeInstallPromptEvent;
		});
		window.addEventListener('appinstalled', () => (this.#event = null));
	}
}

// Created when the app starts, so the event is not missed.
export const install = new Install();
