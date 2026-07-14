<script setup lang="ts">
// Reusable tracklist for albums and releases. A track links to its canonical
// song only through recording_entity_id (the reusable-recording identity) — the
// track placement id and provider ids never drive navigation.
const props = withDefaults(defineProps<{
  tracks?: any[]
  title?: string
  kicker?: string
}>(), { tracks: () => [], title: 'Tracklist', kicker: 'Recordings' })

interface Track { pos: string; title: string; artist: string; duration: string; to?: string; recordingId?: string; lyricsAvailable: boolean }

const rows = computed<Track[]>(() =>
  (props.tracks ?? []).map((track, index) => {
    const recordingId = track.recording_entity_id
    const materialized = recordingId && track.resolution_state !== 'unresolved'
    return {
      pos: formatValue(track.position ?? track.number) || String(index + 1),
      title: formatValue(track.title) || 'Untitled',
      artist: artistCreditLine(track.artist_credits),
      duration: formatDuration(track.length_ms ?? track.duration_ms),
		to: materialized ? entityPath({ id: recordingId, kind: 'recording' }) : undefined,
		recordingId: materialized ? recordingId : undefined,
		lyricsAvailable: track.lyrics_available === true,
    }
  }),
)

// Plain anchors (not NuxtLink) so we fully control the click: a normal click
// opens the quick-look drawer; a modifier/middle click falls through to the
// browser and opens the full recording page in a new tab.
const { open } = useSongPanel()
function onTrackClick(event: MouseEvent, track: Track) {
  if (!track.recordingId) return
  if (event.metaKey || event.ctrlKey || event.shiftKey || event.button === 1) return
  event.preventDefault()
  open(track.recordingId)
}
</script>

<template>
  <OverviewPanel v-if="rows.length" :title="title" :kicker="kicker" full>
    <ol class="line-list tracklist">
      <li v-for="(track, index) in rows" :key="index">
        <span class="line-list__index">{{ track.pos }}</span>
        <span class="line-list__main">
          <component :is="track.recordingId ? 'a' : 'span'" :href="track.recordingId ? track.to : undefined" class="tracklist__title" :class="{ 'is-link': track.recordingId }" @click="onTrackClick($event, track)">
            {{ track.title }}<template v-if="track.recordingId"> ↗</template>
          </component>
          <span v-if="track.artist" class="line-list__sub">{{ track.artist }}</span>
		</span>
		<span v-if="track.lyricsAvailable" class="tracklist__lyrics" title="Lyrics available">Lyrics</span>
		<span v-if="track.duration" class="line-list__meta">{{ track.duration }}</span>
      </li>
    </ol>
  </OverviewPanel>
</template>

<style scoped>
.tracklist__title.is-link:hover { color: var(--gold); }
.tracklist__lyrics {
  color: var(--gold);
  font: 600 0.58rem / 1 var(--font-mono);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
</style>
