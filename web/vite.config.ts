import { defineConfig, type Plugin } from 'vite'
import { resolve } from 'node:path'

function devVideoRoute(): Plugin {
  return {
    name: 'dev-video-route',
    configureServer(server) {
      server.middlewares.use((req, _res, next) => {
        if (req.url === '/video' || req.url === '/video/') {
          req.url = '/video.html'
        }
        next()
      })
    },
  }
}

export default defineConfig({
  plugins: [devVideoRoute()],
  build: {
    rollupOptions: {
      input: {
        main: resolve(__dirname, 'index.html'),
        video: resolve(__dirname, 'video.html'),
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:8080',
      '/videos': 'http://127.0.0.1:8080',
      '/examples': 'http://127.0.0.1:8080',
    },
  },
})
