<script lang="ts">
	let {
		legend,
		items,
		selected = $bindable(),
		empty
	}: {
		legend: string;
		items: { id: number; label: string }[];
		selected: number[];
		/** Shown when there is nothing to choose from. */
		empty: string;
	} = $props();

	function toggle(id: number, on: boolean) {
		selected = on ? [...selected, id] : selected.filter((s) => s !== id);
	}
</script>

<fieldset class="checklist">
	<legend>{legend}</legend>
	{#if items.length === 0}
		<p class="hint">{empty}</p>
	{:else}
		<ul>
			{#each items as item (item.id)}
				<li>
					<label>
						<input
							type="checkbox"
							checked={selected.includes(item.id)}
							onchange={(e) => toggle(item.id, e.currentTarget.checked)}
						/>
						<span>{item.label}</span>
					</label>
				</li>
			{/each}
		</ul>
	{/if}
</fieldset>

<style>
	ul {
		max-height: 260px;
		margin: 0;
		padding: 4px 0;
		overflow-y: auto;
		list-style: none;
		border: 1px solid var(--field-line);
		border-radius: var(--radius);
		background: var(--surface);
	}

	label {
		display: flex;
		align-items: center;
		gap: 8px;
		min-height: 30px;
		padding: 2px 10px;
		cursor: pointer;
	}

	label:hover {
		background: var(--row);
	}

	span {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	@media (pointer: coarse) {
		label {
			min-height: 44px;
		}
	}
</style>
