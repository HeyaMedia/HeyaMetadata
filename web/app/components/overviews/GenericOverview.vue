<script setup lang="ts">
import type { Fact } from '~/components/FactList.vue'
import type { EntityDocument } from '~/utils/types'

// Fallback overview for unrecognized / future kinds and simple editions. Renders
// the scalar-ish data fields as facts, plus external IDs and any ratings/links.
const props = defineProps<{ entity: EntityDocument }>()

// Structured collections are rendered by dedicated panels or omitted here.
const SKIP = new Set([
  'images', 'titles', 'names', 'overviews', 'descriptions', 'biographies', 'editions',
  'credits', 'episodes', 'tracks', 'videos', 'recommendations', 'ratings', 'artist_credits',
  'relationships', 'similar_artists', 'annotations', 'tags', 'links', 'areas', 'media',
  'releases', 'seasons', 'labels', 'sources', 'metrics', 'collection', 'classification',
])

const facts = computed<Fact[]>(() => {
  const data = props.entity.data ?? {}
  return Object.entries(data)
    .filter(([key]) => !SKIP.has(key))
    .map(([key, value]) => ({ label: formatKey(key), value }))
})

const genres = computed(() => {
  const data: any = props.entity.data ?? {}
  const raw = data.genres ?? data.classification?.genres ?? data.subjects
  if (!Array.isArray(raw)) return []
  return raw.map((item: any) => formatValue(item)).filter(Boolean).slice(0, 18)
})

const ratings = computed(() => (props.entity.data as any)?.ratings)
const links = computed(() => (props.entity.data as any)?.links)
</script>

<template>
  <div class="overview-grid">
    <OverviewPanel title="What Heya knows" kicker="Combined record">
      <FactList :facts="facts" />
      <div v-if="genres.length" class="chip-row generic-genres">
        <span v-for="genre in genres" :key="genre" class="chip">{{ genre }}</span>
      </div>
    </OverviewPanel>

    <ExternalIdsPanel :external-ids="entity.external_ids" />
    <LinksList :links="links" />
    <RatingsPanel :ratings="ratings" />
  </div>
</template>

<style scoped>
.generic-genres { margin-top: 1.1rem; }
</style>
