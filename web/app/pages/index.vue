<script setup lang="ts">
import type { CardShape } from '~/utils/kinds'
import type { CollectionCard, EntitySummary, LibraryStats } from '~/utils/types'

// Home is the only page that skips the global "· Heya" title suffix, so it pins
// its own standalone template and a bare "Heya" title.
useHead({ titleTemplate: '%s' })
useSeoMeta({
  title: 'Heya',
  description: 'The canonical library of reconciled movie, TV, anime, music, book, and manga metadata — search it, inspect every upstream source, and audit the result.',
  twitterCard: 'summary_large_image',
})

// Homepage = library overview, not a search screen. Each shelf is fetched
// independently (its own latest?kind= call) so a burst of one domain can never
// crowd out the others. Shelves prefer root entities.
const api = useHeyaApi()
const { signature } = useLocale()
const localeSignature = computed(signature)

interface Shelf {
  key: string
  title: string
  kicker: string
  kind: string
  shape: CardShape
  to: string
}

const SHELVES: Shelf[] = [
  { key: 'movie', title: 'Movies', kicker: 'Recently updated', kind: 'movie', shape: 'poster', to: '/movies' },
  { key: 'tv_show', title: 'Television', kicker: 'Recently updated', kind: 'tv_show', shape: 'poster', to: '/tv' },
  { key: 'anime', title: 'Anime', kicker: 'Recently updated', kind: 'anime', shape: 'poster', to: '/anime' },
  { key: 'artist', title: 'Artists', kicker: 'Music', kind: 'artist', shape: 'portrait', to: '/music' },
  { key: 'release_group', title: 'Albums', kicker: 'Music', kind: 'release_group', shape: 'square', to: '/music' },
  { key: 'book_work', title: 'Books', kicker: 'Written', kind: 'book_work', shape: 'poster', to: '/books' },
  { key: 'manga', title: 'Manga', kicker: 'Written', kind: 'manga', shape: 'poster', to: '/manga' },
  { key: 'manga_volume', title: 'Manga volumes', kicker: 'Written', kind: 'manga_volume', shape: 'poster', to: '/manga' },
]

// Do not suspend the initial SPA mount on the complete library overview. The
// hero and loading shelf should paint immediately while these independent
// reads settle in the background.
const { data, pending } = useLazyAsyncData(() => `home:${localeSignature.value}`, async () => {
  const [shelves, collectionData, stats] = await Promise.all([
    Promise.all(SHELVES.map(shelf => api.latest(shelf.kind, 12).then(r => r.results ?? []).catch(() => []))),
    api.collections().then(r => r.collections ?? []).catch(() => []),
    api.stats().catch(() => ({} as LibraryStats)),
  ])
  const byKey: Record<string, EntitySummary[]> = {}
  SHELVES.forEach((shelf, index) => { byKey[shelf.key] = shelves[index] })
  return { byKey, collections: collectionData as CollectionCard[], stats: stats as LibraryStats }
}, {
  default: () => ({ byKey: {}, collections: [] as CollectionCard[], stats: {} as LibraryStats }),
  getCachedData: sessionCached,
})

const visibleShelves = computed(() => SHELVES.filter(shelf => (data.value?.byKey[shelf.key]?.length ?? 0) > 0))
const stats = computed(() => data.value?.stats ?? {})
const collections = computed(() => data.value?.collections ?? [])
const providerCount = computed(() => Object.keys(stats.value.provider_claims ?? {}).length)
</script>

<template>
  <div class="shell page home">
    <section class="home-hero">
      <div class="home-hero__intro">
        <span class="section-label">The canonical library</span>
        <h1 class="editorial">Everything Heya<br><em>knows right now.</em></h1>
        <p>Recently combined movies, television, music, books, anime, and manga — every source still attached.</p>
      </div>
      <GlobalSearch class="home-hero__search" size="hero" />
      <dl v-if="stats.entities" class="home-coverage">
        <div><dt>Canonical entities</dt><dd>{{ formatCount(stats.entities) }}</dd></div>
        <div><dt>Identity providers</dt><dd>{{ providerCount }}</dd></div>
        <div><dt>Cached images</dt><dd>{{ formatCount(stats.materialized_images) }}</dd></div>
        <NuxtLink to="/stats" class="home-coverage__link">Full coverage ↗</NuxtLink>
      </dl>
    </section>

    <template v-if="pending">
      <section class="rail">
        <div class="rail__head"><div><span class="section-label">Loading</span><h2>The library</h2></div></div>
        <LoadingSkeleton layout="rail" shape="poster" :count="8" />
      </section>
    </template>

    <template v-else>
      <MediaRail
        v-for="shelf in visibleShelves"
        :key="shelf.key"
        :title="shelf.title"
        :kicker="shelf.kicker"
        :shape="shelf.shape"
        :items="data?.byKey[shelf.key]"
        :browse-to="shelf.to"
      />

      <MediaRail
        v-if="collections.length"
        title="Movie collections"
        kicker="Franchises"
        shape="landscape"
        browse-to="/collections"
        browse-label="See collections"
      >
        <CollectionCard v-for="item in collections.slice(0, 8)" :key="item.provider_id" :collection="item" />
      </MediaRail>

      <EmptyState
        v-if="!visibleShelves.length && !collections.length"
        title="The library is empty."
        message="Resolve an entity from the search workbench to populate the canonical library."
      >
        <NuxtLink to="/search" class="btn btn--gold">Open search</NuxtLink>
      </EmptyState>
    </template>
  </div>
</template>

<style scoped>
.home-hero {
  display: grid;
  grid-template-columns: 1.15fr 0.85fr;
  gap: 1.5rem 4rem;
  align-items: end;
  padding-block: clamp(1.5rem, 4vw, 3rem);
}
.home-hero__intro h1 { margin: 1rem 0 1.1rem; font-size: clamp(2.6rem, 5.5vw, 4.6rem); }
.home-hero__intro p { max-width: 34rem; margin: 0; color: var(--muted); font-size: 0.95rem; line-height: 1.7; }
.home-hero__search { align-self: end; }

.home-coverage {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: clamp(1.5rem, 5vw, 4rem);
  grid-column: 1 / -1;
  margin: 0.5rem 0 0;
  padding-top: 1.5rem;
  border-top: 1px solid var(--line);
}
.home-coverage div { display: flex; flex-direction: column-reverse; gap: 0.25rem; }
.home-coverage dt { color: var(--muted-2); font-size: 0.64rem; }
.home-coverage dd { margin: 0; font-family: var(--font-mono); font-size: 1.3rem; }
.home-coverage__link { margin-left: auto; color: var(--muted); font-size: 0.72rem; }
.home-coverage__link:hover { color: var(--gold); }

@media (max-width: 860px) {
  .home-hero { grid-template-columns: 1fr; align-items: start; }
  .home-hero__search { max-width: 34rem; }
}
</style>
