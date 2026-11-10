<script lang="ts">
	import { shortcuts } from '#lib/shortcuts.svelte.ts';
	import ShortcutList from './ShortcutList.svelte';

	let dialog: HTMLDialogElement | undefined = $state();

	$effect(() => {
		if (!dialog) return;
		if (shortcuts.helpOpen && !dialog.open) dialog.showModal();
		else if (!shortcuts.helpOpen && dialog.open) dialog.close();
	});
</script>

<!-- Opened with ?, closed with Esc or the button. -->
<dialog
	bind:this={dialog}
	aria-labelledby="shortcuts-title"
	onclose={() => (shortcuts.helpOpen = false)}
>
	<h2 id="shortcuts-title">Keyboard shortcuts</h2>
	<ShortcutList />
	<form method="dialog">
		<button class="button">Close</button>
	</form>
</dialog>

<style>
	dialog {
		width: min(100% - 32px, 560px);
		max-height: calc(100dvh - 64px);
		padding: 18px 20px 20px;
		border: 0;
		border-radius: var(--radius-card);
		background: var(--surface);
		color: var(--text);
		box-shadow: var(--shadow-float);
	}

	dialog::backdrop {
		background: var(--backdrop);
	}

	h2 {
		margin: 0;
		font-size: 16px;
	}

	form {
		margin-top: 16px;
		text-align: right;
	}
</style>
