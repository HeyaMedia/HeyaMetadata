<script setup lang="ts">
import type { CardShape } from '~/utils/kinds'
import type { EntitySummary } from '~/utils/types'

useSeoMeta({
  title: 'Music',
  description: 'Browse artists, albums, and recordings assembled from MusicBrainz, Discogs, and storefront evidence.',
  twitterCard: 'summary_large_image',
})

// Music spans several kinds, so this is a curated landing rather than a single
// locked browse. Each section is fetched independently.
const api = useHeyaApi()
const { signature } = useLocale()
const localeSignature = computed(signature)

interface Section { key: string; kind: string; title: string; shape: CardShape }
const SECTIONS: Section[] = [
  { key: 'artist', kind: 'artist', title: 'Artists', shape: 'portrait' },
  { key: 'release_group', kind: 'release_group', title: 'Albums', shape: 'square' },
  { key: 'recording', kind: 'recording', title: 'Recordings', shape: 'square' },
]

const { data, pending } = await useAsyncData(() => `music:${localeSignature.value}`, async () => {
  const lists = await Promise.all(
    SECTIONS.map(section => api.browse({ kind: section.kind, sort: 'updated', limit: 18 }).then(r => r.results ?? []).catch(() => [])),
  )
  const byKey: Record<string, EntitySummary[]> = {}
  SECTIONS.forEach((section, index) => { byKey[section.key] = lists[index] })
  return byKey
}, { default: () => ({}), getCachedData: sessionCached })
</script>

<template>
  <div class="shell page">
    <header class="page-heading">
      <div>
        <span class="section-label">Sound</span>
        <h1>Music</h1>
        <p>Artists, albums, and recordings assembled from MusicBrainz, Discogs, and storefront evidence.</p>
      </div>
    </header>

    <LoadingSkeleton v-if="pending" layout="grid" shape="square" :count="12" />

    <template v-else>
      <section v-for="section in SECTIONS" :key="section.key" class="music-section">
        <div class="music-section__head">
          <h2>{{ section.title }}</h2>
          <NuxtLink :to="`/browse?kind=${section.kind}`" class="btn--link">Browse all ↗</NuxtLink>
        </div>
        <MediaGrid v-if="data?.[section.key]?.length" :shape="section.shape" :items="data[section.key]" />
        <EmptyState v-else :title="`No ${section.title.toLowerCase()} yet.`" />
      </section>
    </template>
  </div>
</template>

<style scoped>
.music-section { margin-top: 2.5rem; }
.music-section__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 1rem;
  padding-bottom: 0.75rem;
  border-bottom: 1px solid var(--line);
}
.music-section__head h2 { margin: 0; font-size: 1.2rem; font-weight: 500; }
</style>
