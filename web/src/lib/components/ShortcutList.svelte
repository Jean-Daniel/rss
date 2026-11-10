<script lang="ts">
	import { SHORTCUTS } from '#lib/keys.ts';
</script>

{#each SHORTCUTS as section (section.title)}
	<h3>{section.title}</h3>
	<table>
		<tbody>
			{#each section.shortcuts as { keys, what } (what)}
				<tr>
					<th scope="row">
						{#each keys as key, i (key)}
							{#if i > 0}<span class="or">or</span>{/if}
							{#each key.split(' ') as part, j (j)}
								{#if j > 0}+{/if}<kbd>{part}</kbd>
							{/each}
						{/each}
					</th>
					<td>{what}</td>
				</tr>
			{/each}
		</tbody>
	</table>
{/each}

<style>
	h3 {
		margin: 14px 0 6px;
		font-size: 11px;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--text-faint);
	}

	/* The same columns in every table. */
	table {
		border-collapse: collapse;
		width: 100%;
		table-layout: fixed;
	}

	th,
	td {
		padding: 4px 0;
		vertical-align: baseline;
		text-align: left;
	}

	th {
		width: 13em;
		padding-right: 12px;
		font-weight: normal;
		white-space: nowrap;
	}

	kbd {
		display: inline-block;
		min-width: 1.6em;
		padding: 1px 5px;
		border: 1px solid var(--field-line);
		border-bottom-width: 2px;
		border-radius: 4px;
		background: var(--surface-tint);
		font: 12px/1.4 var(--font);
		text-align: center;
	}

	.or {
		margin: 0 4px;
		font-size: 12px;
		color: var(--text-faint);
	}
</style>
