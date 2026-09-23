import tailwindcss from "@tailwindcss/vite";

export default defineNuxtConfig({
  compatibilityDate: '2024-04-03',
  devtools: { enabled: true },

  modules: [
    '@vite-pwa/nuxt',
    '@pinia/nuxt',
    'reka-ui/nuxt',
    'pinia-plugin-persistedstate/nuxt'
  ],
  piniaPluginPersistedstate: {
    storage: 'cookies',
    cookieOptions: {
      sameSite: 'lax',
    },
    debug: true,
  },

  runtimeConfig: {
    public: {
      apiBase: process.env.NUXT_PUBLIC_API_BASE || 'http://localhost:8080',
    },
  },

  pwa: {
    registerType: 'autoUpdate',
    manifest: {
      name: 'Touchline',
      short_name: 'Touchline',
      description: 'Persistent multiplayer football management universe',
      theme_color: '#1a237e',
      background_color: '#0f172a',
      display: 'standalone',
      icons: [],
    },
    workbox: {
      navigateFallback: '/',
      globPatterns: ['**/*.{js,css,html,png,svg,ico}'],
    },
    devOptions: {
      enabled: true,
      type: 'module',
    },
  },

  routeRules: {
    '/play/**/*': { ssr: false },
    '/admin/**/*': { ssr: false },
  },

  css: ['~/assets/css/main.css'],

  vite: {
    plugins: [
      tailwindcss()
    ]
  },

  app: {
    head: {
      title: 'Touchline',
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
        { name: 'description', content: 'The persistent multiplayer football universe.' },
      ],
    },
  },
})