export default defineNuxtConfig({
  compatibilityDate: '2026-07-10',
  devtools: { enabled: true },
  ssr: false,

  app: {
    head: {
      title: 'Heya Metadata',
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
        { name: 'color-scheme', content: 'dark' },
        { name: 'theme-color', content: '#111827' },
      ],
    },
  },
})
