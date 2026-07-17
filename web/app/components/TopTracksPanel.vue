<script setup lang="ts">
import type { TopTrack } from '~/utils/types'

// Popular tracks for an artist from /entities/{id}/top-tracks. A resolved track
// (recording_entity_id) links to its canonical recording; an unresolved one is
// still shown but only ever links out to the provider's own page — never a
// fabricated recording route. Paginated in place with "Show more".
const props = defineProps<{ entityId: string }>()

const api = useHeyaApi()
const PAGE = 50

const { data: first, pending } = useAsyncData(
  () => `top-tracks:${props.entityId}`,
  () => api.topTracks(props.entityId, { offset: 0, limit: PAGE }).catch(() => null),
  { getCachedData: sessionCached },
)

// Appended pages accumulate here and reset whenever the artist changes.
const extra = ref<TopTrack[]>([])
watch(() => props.entityId, () => { extra.value = [] })

const tracks = computed<TopTrack[]>(() => [...(first.value?.results ?? []), ...extra.value])
const total = computed(() => first.value?.total ?? tracks.value.length)
const sources = computed(() => first.value?.sources ?? [])
const done = computed(() => tracks.value.length >= total.value)

const sourceLine = computed(() => {
  const names = [...new Set(sources.value.map(s => s.provider).filter(Boolean))].map(formatKey)
  return names.length ? `via ${names.join(', ')}` : ''
})

const loadingMore = ref(false)
async function loadMore() {
  if (loadingMore.value || done.value) return
  loadingMore.value = true
  try {
    const res = await api.topTracks(props.entityId, { offset: tracks.value.length, limit: PAGE })
    extra.value.push(...(res.results ?? []))
  }
  finally { loadingMore.value = false }
}

function metric(track: TopTrack): string {
  if (typeof track.playcount === 'number') return `${formatCount(track.playcount)} plays`
  if (typeof track.listeners === 'number') return `${formatCount(track.listeners)} listeners`
  return ''
}

// A track links to its canonical recording only when materialized; otherwise it
// stays display-only (linking out to the provider page when a url exists).
function recordingLink(track: TopTrack): string | undefined {
  if (!track.recording_entity_id || track.resolution_state === 'unresolved') return undefined
  return `/recordings/${track.recording_entity_id}`
}

// Plain anchors (not NuxtLink) so we fully control the click: a normal click
// opens the quick-look drawer; a modifier/middle click falls through to the
// browser and opens the full recording page in a new tab.
const { open } = useSongPanel()
function onTrackClick(event: MouseEvent, track: TopTrack) {
  if (!recordingLink(track)) return
  if (event.metaKey || event.ctrlKey || event.shiftKey || event.button === 1) return
  event.preventDefault()
  open(track.recording_entity_id)
}
</script>

<template>
  <section v-if="pending || tracks.length" class="tracks">
    <header class="section-head">
      <div><span class="section-label">Popularity</span><h2>Popular tracks</h2></div>
      <span v-if="sourceLine" class="tracks__source">{{ sourceLine }}</span>
    </header>

    <p v-if="pending" class="muted">Loading popular tracks…</p>

    <template v-else>
      <ol class="tracks__list">
        <li v-for="track in tracks" :key="`${track.rank}:${track.title}`" class="tracks__row">
          <span class="tracks__rank">{{ track.rank }}</span>
          <component
            :is="recordingLink(track) || track.url ? 'a' : 'span'"
            :href="recordingLink(track) || track.url || undefined"
            :target="!recordingLink(track) && track.url ? '_blank' : undefined"
            :rel="!recordingLink(track) && track.url ? 'noopener noreferrer' : undefined"
            class="tracks__title"
            :class="{ 'is-canonical': recordingLink(track), 'is-ghost': !recordingLink(track) && !track.url }"
            @click="onTrackClick($event, track)"
          >
            {{ track.title }}<template v-if="!recordingLink(track) && track.url"> ↗</template>
          </component>
          <span v-if="metric(track)" class="tracks__metric">{{ metric(track) }}</span>
        </li>
      </ol>

      <button v-if="!done" type="button" class="btn btn--sm tracks__more" :disabled="loadingMore" @click="loadMore">
        {{ loadingMore ? 'Loading…' : `Show more (${formatCount(total - tracks.length)} left)` }}
      </button>
    </template>
  </section>
</template>

<style scoped>
.tracks { margin-top: clamp(1.75rem, 3vw, 2.5rem); }
.section-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1.1rem;
  padding-bottom: 0.75rem;
  border-bottom: 1px solid var(--line);
}
.section-head h2 { margin: 0.3rem 0 0; font-size: 1.2rem; font-weight: 500; }
.tracks__source { color: var(--muted-2); font-family: var(--font-mono); font-size: 0.66rem; }
.tracks__list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 0 2rem;
  margin: 0;
  padding: 0;
  list-style: none;
  counter-reset: none;
}
.tracks__row {
  display: grid;
  grid-template-columns: 2rem 1fr auto;
  align-items: baseline;
  gap: 0.75rem;
  padding: 0.4rem 0;
  border-top: 1px solid var(--line-soft);
  font-size: 0.76rem;
}
.tracks__rank { color: var(--muted-2); font-family: var(--font-mono); font-size: 0.68rem; text-align: right; }
.tracks__title { min-width: 0; overflow: hidden; color: var(--text-dim); text-overflow: ellipsis; white-space: nowrap; }
.tracks__title.is-canonical:hover, .tracks__title[href]:hover { color: var(--gold); }
.tracks__title.is-ghost { color: var(--muted); }
.tracks__metric { flex: 0 0 auto; color: var(--muted-2); font-family: var(--font-mono); font-size: 0.64rem; white-space: nowrap; }
.tracks__more { margin-top: 1.25rem; }
</style>
