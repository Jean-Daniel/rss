// The rules for reading shortcut keys: npm test (Node's test runner).
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { commandFor, SHORTCUTS, type Command, type Press } from './keys.ts';

const body = { tagName: 'BODY', closest: () => null };
const press = (key: string, more: Partial<Press> = {}): Press => ({
	key,
	code: '',
	shiftKey: false,
	ctrlKey: false,
	metaKey: false,
	altKey: false,
	repeat: false,
	isComposing: false,
	target: body,
	...more
});

test('letters, with and without Shift', () => {
	assert.equal(commandFor(press('j')), 'next');
	assert.equal(commandFor(press('J', { shiftKey: true })), 'next'); // no Shift meaning: the same
	assert.equal(commandFor(press('J')), 'next'); // Caps Lock
	assert.equal(commandFor(press('r')), 'refresh');
	assert.equal(commandFor(press('R', { shiftKey: true })), 'refreshShown');
	assert.equal(commandFor(press('a')), 'markRead');
	assert.equal(commandFor(press('A', { shiftKey: true })), 'markAllRead');
	assert.equal(commandFor(press('V', { shiftKey: true })), 'visitAndRead');
	assert.equal(commandFor(press('ArrowRight')), 'open');
	assert.equal(commandFor(press('q')), null);
	assert.equal(commandFor(press('Dead')), null); // an accent key
});

test('other keys of the browser and the system are left alone', () => {
	for (const mod of ['ctrlKey', 'metaKey', 'altKey'] as const) {
		assert.equal(commandFor(press('r', { [mod]: true })), null, mod);
		assert.equal(commandFor(press('s', { [mod]: true })), null, mod);
	}
	assert.equal(commandFor(press('j', { isComposing: true })), null);
});

test('only moving keys repeat', () => {
	assert.equal(commandFor(press('j', { repeat: true })), 'next');
	assert.equal(commandFor(press(' ', { repeat: true })), 'pageDown');
	assert.equal(commandFor(press('a', { repeat: true })), null);
	assert.equal(commandFor(press('r', { repeat: true })), null);
});

test('typing in a field', () => {
	for (const tagName of ['INPUT', 'TEXTAREA', 'SELECT']) {
		assert.equal(commandFor(press('j', { target: { tagName } })), null, tagName);
		assert.equal(commandFor(press('Escape', { target: { tagName } })), 'escape', tagName);
	}
	assert.equal(
		commandFor(press('j', { target: { tagName: 'DIV', isContentEditable: true } })),
		null
	);
});

test('Space presses buttons, not links', () => {
	const button = { tagName: 'BUTTON', closest: (s: string) => (s.includes('button') ? {} : null) };
	const link = { tagName: 'A', closest: () => null };
	assert.equal(commandFor(press(' ', { target: button })), null);
	assert.equal(commandFor(press('j', { target: button })), 'next');
	assert.equal(commandFor(press(' ', { target: link })), 'pageDown');
	assert.equal(commandFor(press(' ', { shiftKey: true, target: link })), 'pageUp');
});

test('digits and symbols on other layouts', () => {
	assert.equal(commandFor(press('2', { code: 'Digit2' })), 'allItems');
	assert.equal(commandFor(press('é', { code: 'Digit2' })), 'allItems'); // AZERTY, no Shift
	assert.equal(commandFor(press('"', { code: 'Digit3' })), 'saved'); // AZERTY
	assert.equal(commandFor(press('/', { code: 'Digit7', shiftKey: true })), 'focusSearch'); // QWERTZ
	assert.equal(commandFor(press('?', { code: 'Slash', shiftKey: true })), 'help');
	assert.equal(commandFor(press('?', { code: 'KeyM', shiftKey: true })), 'help'); // AZERTY
	assert.equal(commandFor(press('5', { code: 'Numpad5' })), 'search');
	// NumLock off: the keypad's arrows and Page Down are not digits.
	assert.equal(commandFor(press('ArrowDown', { code: 'Numpad2' })), null);
	assert.equal(commandFor(press('PageDown', { code: 'Numpad3' })), null);
	assert.equal(commandFor(press('Clear', { code: 'Numpad5' })), null);
});

test('the help lists every command, each key once', () => {
	const all: Command[] = [
		'next',
		'previous',
		'pageDown',
		'pageUp',
		'open',
		'save',
		'visit',
		'visitAndRead',
		'toggleRead',
		'markRead',
		'markAllRead',
		'undoReads',
		'allItems',
		'saved',
		'search',
		'focusSearch',
		'feeds',
		'refresh',
		'refreshShown',
		'addFeed',
		'addGroup',
		'edit',
		'settings',
		'help',
		'escape'
	];
	const names = SHORTCUTS.flatMap((s) => s.shortcuts.flatMap((k) => Object.keys(k.commands)));
	const listed = SHORTCUTS.flatMap((s) => s.shortcuts.flatMap((k) => Object.values(k.commands)));
	assert.equal(new Set(names).size, names.length);
	assert.deepEqual(new Set(listed), new Set(all));
});
