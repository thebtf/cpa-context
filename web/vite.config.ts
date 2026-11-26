import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// One self-contained file pair: the plugin embeds the build into the Go binary
// and serves it as a browser resource, so no asset requests may escape it.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    assetsInlineLimit: 100 * 1024 * 1024,
    cssCodeSplit: false,
    rollupOptions: {
      output: {
        entryFileNames: 'dashboard.js',
        chunkFileNames: 'dashboard-[name].js',
        assetFileNames: 'dashboard.[ext]',
        inlineDynamicImports: true,
      },
    },
  },
})
