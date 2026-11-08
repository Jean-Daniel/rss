import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

// During development the Go server runs on the host; inside the Node container
// it is reached through host.containers.internal (see ./npm).
const apiTarget = process.env.API_URL ?? 'http://localhost:8080';

export default defineConfig({
	plugins: [
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			// A single-page app: the Go server embeds the build and answers every
			// route that is not a file with index.html. Brotli and gzip copies are
			// made at build time so the server never compresses on the fly.
			adapter: adapter({ fallback: 'index.html', precompress: true })
		})
	],
	server: {
		// Keep the browser's Host header: the Go server compares it with the
		// Origin of state-changing requests.
		proxy: { '/api': { target: apiTarget, changeOrigin: false } },
		watch: process.env.VITE_POLL ? { usePolling: true, interval: 300 } : undefined
	}
});
