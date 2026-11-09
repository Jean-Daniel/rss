import type { Attachment } from 'svelte/attachments';

/**
 * Lets a drawer that slides in from the left be swiped away: it follows the
 * finger and closes when let go past a third of its width, or on a quick
 * flick. Vertical moves are left to scrolling.
 */
export function swipeToClose(close: () => void): Attachment<HTMLElement> {
	return (el) => {
		let state: 'idle' | 'undecided' | 'swiping' = 'idle';
		let startX = 0;
		let startY = 0;
		let startTime = 0;
		let dx = 0;

		function start(event: TouchEvent) {
			if (event.touches.length !== 1) {
				cancel();
				return;
			}
			const touch = event.touches[0];
			state = 'undecided';
			startX = touch.clientX;
			startY = touch.clientY;
			startTime = event.timeStamp;
			dx = 0;
		}

		function move(event: TouchEvent) {
			if (state === 'idle') return;
			const touch = event.touches[0];
			const x = touch.clientX - startX;
			const y = touch.clientY - startY;
			if (state === 'undecided') {
				if (Math.abs(x) < 10 && Math.abs(y) < 10) return;
				if (x >= 0 || Math.abs(x) <= Math.abs(y)) {
					state = 'idle'; // a scroll, or a swipe the wrong way
					return;
				}
				state = 'swiping';
				el.style.transition = 'none';
			}
			dx = Math.min(0, x);
			el.style.transform = `translateX(${dx}px)`;
		}

		function end(event: TouchEvent) {
			if (state !== 'swiping') {
				state = 'idle';
				return;
			}
			const flick = dx < -30 && event.timeStamp - startTime < 250;
			const far = dx < -el.offsetWidth / 3;
			cancel();
			// The stylesheet's transition then slides the drawer out from
			// where the finger left it, or back in.
			if (flick || far) close();
		}

		function cancel() {
			state = 'idle';
			el.style.transition = '';
			el.style.transform = '';
		}

		const passive = { passive: true };
		el.addEventListener('touchstart', start, passive);
		el.addEventListener('touchmove', move, passive);
		el.addEventListener('touchend', end, passive);
		el.addEventListener('touchcancel', cancel, passive);
		return () => {
			cancel();
			el.removeEventListener('touchstart', start);
			el.removeEventListener('touchmove', move);
			el.removeEventListener('touchend', end);
			el.removeEventListener('touchcancel', cancel);
		};
	};
}
