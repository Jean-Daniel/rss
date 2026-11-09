<script lang="ts">
	import '#lib/styles/app.css';
	import UpdateBanner from '#lib/components/UpdateBanner.svelte';
	// Listens for the browser's install offer from the start (see install.svelte.ts).
	import '#lib/install.svelte.ts';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();
</script>

<!-- iOS Safari shows :active (pressed) styles only to pages listening for touches. -->
<svelte:document ontouchstart={() => {}} />

{@render children()}
<UpdateBanner />
