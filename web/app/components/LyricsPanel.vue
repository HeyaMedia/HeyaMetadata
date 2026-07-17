<script setup lang="ts">
import type { LyricDocument } from '~/utils/types'

// Lyrics for a recording, fetched from /recordings/{id}/lyrics at runtime. The
// text is provider-supplied (e.g. LRCLIB) and rendered from the response — no
// lyric content lives in this component. Collapsed by default.
const props = defineProps<{ recordingId: string }>()

const api = useHeyaApi()
const { data } = useAsyncData(
  () => `lyrics:${props.recordingId}`,
  () => api.recordingLyrics(props.recordingId).then(r => r.items ?? []).catch(() => [] as LyricDocument[]),
  { default: () => [] as LyricDocument[], getCachedData: sessionCached },
)

const lyric = computed<LyricDocument | undefined>(() =>
  data.value.find(item => item.plain_lyrics || item.synced_lyrics) ?? data.value[0],
)
const expanded = ref(false)
</script>

<template>
  <OverviewPanel v-if="lyric" title="Lyrics" kicker="Text" full>
    <div class="lyrics__meta">
      <span v-if="lyric.provider" class="chip chip--accent">{{ formatKey(lyric.provider) }}</span>
      <span v-if="lyric.instrumental" class="chip">Instrumental</span>
      <span v-if="lyric.synced_lyrics" class="chip">Synced available</span>
    </div>
    <template v-if="lyric.plain_lyrics">
      <pre class="lyrics__text" :class="{ 'is-clamped': !expanded }">{{ lyric.plain_lyrics }}</pre>
      <button type="button" class="btn--link lyrics__toggle" @click="expanded = !expanded">
        {{ expanded ? 'Collapse' : 'Show full lyrics' }}
      </button>
    </template>
    <p v-else-if="lyric.instrumental" class="muted">This recording is instrumental.</p>
  </OverviewPanel>
</template>

<style scoped>
.lyrics__meta { display: flex; flex-wrap: wrap; gap: 0.5rem; margin-bottom: 1rem; }
.lyrics__text {
  margin: 0;
  color: var(--text-dim);
  font: 0.82rem / 1.75 var(--font-sans);
  white-space: pre-wrap;
}
.lyrics__text.is-clamped {
  display: -webkit-box;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 8;
  -webkit-mask-image: linear-gradient(to bottom, #000 60%, transparent);
}
.lyrics__toggle { margin-top: 0.75rem; color: var(--gold); }
.muted { font-size: 0.8rem; }
</style>
