export default defineNuxtConfig({
  compatibilityDate: '2026-07-10',
  devtools: { enabled: true },
  ssr: false,

  css: ['~/assets/css/main.css'],

  // Name components by filename regardless of nesting (so overviews/MovieOverview
  // resolves as <MovieOverview>, not <OverviewsMovieOverview>).
  components: [{ path: '~/components', pathPrefix: false }],

  modules: ['@nuxtjs/seo'],

  // Canonical production origin. Drives canonical <link>, og:url, and schema.org
  // @id/url. Overridable at build via NUXT_PUBLIC_SITE_URL. NOTE: this app is a
  // pure SPA (ssr:false) so client-injected tags only reach JS-executing crawlers
  // (Google). Non-JS social scrapers (Discord/Slack/Twitter/FB/LinkedIn) and the
  // authoritative robots.txt + sitemap.xml are served by the same-origin Go server
  // (internal/server/web.go), which renders per-route <head> meta into index.html.
  site: {
    url: 'https://heya.media',
    name: 'Heya',
    description: 'Canonical, provenance-aware metadata for movies, TV, anime, music, books, and manga — every source still attached.',
  },

  // Module ownership split (see memory: seo-strategy):
  //  - ogImage: a SPA has no server to render OG images at request time; per-page
  //    og:image points at the entity's own artwork variant instead.
  //  - sitemap/robots: owned by the Go server (DB-backed enumeration of every
  //    canonical entity); the build-time Nuxt versions would only see static routes.
  //  - linkChecker: dev-only build noise we don't need.
  // Kept: nuxt-seo-utils (owns the "%s · Heya" title template + auto og:title/
  //  og:description mirroring via automaticDefaults) and nuxt-schema-org.
  ogImage: { enabled: false },
  sitemap: { enabled: false },
  robots: { enabled: false },
  linkChecker: { enabled: false },

  schemaOrg: {
    identity: {
      type: 'Organization',
      name: 'Heya',
      url: 'https://heya.media',
    },
  },

  app: {
    head: {
      // Title template is owned by nuxt-seo-utils (site.name "Heya"); we only pin
      // the separator so titles render as "Page Title · Heya".
      templateParams: { separator: '·' },
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
        { name: 'color-scheme', content: 'dark' },
        { name: 'theme-color', content: '#0b0e10' },
      ],
    },
  },
})
