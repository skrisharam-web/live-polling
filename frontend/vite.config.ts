import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    strictPort: true,
  },
  build: {
    // Source maps make a production bug reproducible without shipping the
    // original sources to the browser as separate files.
    sourcemap: true,
  },
})
