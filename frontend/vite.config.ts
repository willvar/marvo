import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const apiTarget = process.env.VITE_API_TARGET || 'http://localhost:5090'
const apiProxy = {
  '/api': {
    target: apiTarget,
    changeOrigin: true,
  },
  '/static': {
    target: apiTarget,
    changeOrigin: true,
  },
}

export default defineConfig({
  plugins: [vue()],
  base: '/',
  build: {
    // The Android app may run on an OEM WebView that updates more slowly than
    // desktop Chromium. Keep both JS and minified CSS compatible with the
    // Chrome 99 WebView used by the supported test device. In particular,
    // newer CSS minifiers otherwise rewrite `max-width` media queries to range
    // syntax that this WebView parses as `not all`.
    target: ['chrome99', 'safari15'],
    cssTarget: ['chrome99', 'safari15'],
  },
  server: {
    port: 5080,
    host: '0.0.0.0',
    proxy: apiProxy,
  },
  preview: {
    port: 5080,
    host: '0.0.0.0',
    proxy: apiProxy,
  },
})
