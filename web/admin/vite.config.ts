import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, '.', '')
  const backendTarget = env.VITE_BACKEND_TARGET || 'http://127.0.0.1:8080'

  return {
    base: '/admin/',
    plugins: [vue()],
    server: {
      proxy: {
        '/api': {
          target: backendTarget,
          changeOrigin: false,
        },
        '/media': {
          target: backendTarget,
          changeOrigin: false,
        },
        '/articles': {
          target: backendTarget,
          changeOrigin: false,
        },
        '/projects': {
          target: backendTarget,
          changeOrigin: false,
        },
        '/about': {
          target: backendTarget,
          changeOrigin: false,
        },
        '/assets': {
          target: backendTarget,
          changeOrigin: false,
        },
      },
    },
  }
})
