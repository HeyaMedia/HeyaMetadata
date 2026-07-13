<script setup lang="ts">
// Reusable tracklist for albums and releases. Links a track to its canonical
// recording when the backend provides recording_entity_id (see backend wishlist).
const props = withDefaults(defineProps<{
  tracks?: any[]
  title?: string
  kicker?: string
}>(), { tracks: () => [], title: 'Tracklist', kicker: 'Recordings' })

interface Track { pos: string; title: string; artist: string; duration: string; to?: string }

const rows = computed<Track[]>(() =>
  (props.tracks ?? []).map((track, index) => {
    const recordingId = track.recording_entity_id ?? track.recording_id ?? track.entity_id
    return {
      pos: formatValue(track.position ?? track.number) || String(index + 1),
      title: formatValue(track.title) || 'Untitled',
      artist: artistCreditLine(track.artist_credits),
      duration: formatDuration(track.length_ms ?? track.duration_ms),
      to: recordingId ? entityPath({ id: recordingId, kind: 'recording' }) : undefined,
    }
  }),
)
const linkTag = resolveComponent('NuxtLink')
</script>

<template>
  <OverviewPanel v-if="rows.length" :title="title" :kicker="kicker" full>
    <ol class="line-list tracklist">
      <li v-for="(track, index) in rows" :key="index">
        <span class="line-list__index">{{ track.pos }}</span>
        <span class="line-list__main">
          <component :is="track.to ? linkTag : 'span'" :to="track.to" class="tracklist__title" :class="{ 'is-link': track.to }">
            {{ track.title }}<template v-if="track.to"> ↗</template>
          </component>
          <span v-if="track.artist" class="line-list__sub">{{ track.artist }}</span>
        </span>
        <span v-if="track.duration" class="line-list__meta">{{ track.duration }}</span>
      </li>
    </ol>
  </OverviewPanel>
</template>

<style scoped>
.tracklist__title.is-link:hover { color: var(--gold); }
</style>
