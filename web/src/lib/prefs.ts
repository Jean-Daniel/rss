// Reading preferences remembered on this device. The server's defaults apply
// until the reader changes them here.

const SHOW_READ = 'fever.showRead';

/** Whether lists include read items, not only unread ones. */
export function showReadPref(fallback: boolean): boolean {
	try {
		const v = localStorage.getItem(SHOW_READ);
		return v === null ? fallback : v === '1';
	} catch {
		return fallback;
	}
}

export function setShowReadPref(show: boolean) {
	try {
		localStorage.setItem(SHOW_READ, show ? '1' : '0');
	} catch {
		// storage unavailable (private mode): the change lasts for this view
	}
}
