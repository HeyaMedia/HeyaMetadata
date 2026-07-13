export default defineNuxtConfig({
  compatibilityDate: '2026-07-10',
  devtools: { enabled: true },
  ssr: false,

  css: ['~/assets/css/main.css'],

  // Name components by filename regardless of nesting (so overviews/MovieOverview
  // resolves as <MovieOverview>, not <OverviewsMovieOverview>).
  components: [{ path: '~/components', pathPrefix: false }],

  app: {
    head: {
      title: 'Heya Metadata Observatory',
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
        { name: 'color-scheme', content: 'dark' },
        { name: 'theme-color', content: '#0b0e10' },
        { name: 'description', content: 'Inspect canonical metadata assembled by Heya.' },
      ],
    },
  },
})
