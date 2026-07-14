<script setup lang="ts">
import type { EntityDocument } from '~/utils/types'

// Quick-look song drawer. Opened from any track list via useSongPanel(); fetches
// the canonical recording document (same source as the recording page) and shows
// what we know plus lyrics (LyricsPanel, only when present). A single instance is
// mounted at the app root. Full detail stays one click away on the recording page.
const { recordingId, close } = useSongPanel()
const api = useHeyaApi()

const doc = ref<EntityDocument | null>(null)
const loading = ref(false)
const failed = ref(false)

watch(recordingId, async (id) => {
  doc.value = null
  failed.value = false
  if (!id) return
  loading.value = true
  try {
    doc.value = await api.entity(id)
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
})

const data = computed<any>(() => doc.value?.data ?? {})
const title = computed(() => entityTitle(doc.value) || 'Recording')
const artist = computed(() => artistCreditLine(data.value.artist_credits))
const releases = computed<any[]>(() => (Array.isArray(data.value.releases) ? data.value.releases : []))

const chips = computed<string[]>(() => {
  const d = data.value
  const out: string[] = []
  const duration = formatDuration(d.duration_ms)
  if (duration) out.push(duration)
  if (d.rating?.value != null) out.push(`★ ${d.rating.value}`)
  if (d.provider) out.push(formatKey(d.provider))
  const isrcs = Array.isArray(d.isrcs) ? d.isrcs.length : 0
  if (isrcs) out.push(`${isrcs} ISRC${isrcs > 1 ? 's' : ''}`)
  return out
})

function onKey(event: KeyboardEvent) {
  if (event.key === 'Escape') close()
}
watch(recordingId, value => { if (import.meta.client) document.body.style.overflow = value ? 'hidden' : '' })
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  if (import.meta.client) document.body.style.overflow = ''
})
</script>

<template>
  <Teleport to="body">
    <Transition name="song">
      <div v-if="recordingId" class="song" role="dialog" aria-modal="true" aria-label="Song details">
        <div class="song__scrim" @click="close" />
        <aside class="song__drawer">
          <header class="song__head">
            <div class="song__heading">
              <span class="section-label">Song</span>
              <h2 class="song__title">{{ loading ? 'Loading…' : title }}</h2>
              <p v-if="artist" class="song__artist">{{ artist }}</p>
            </div>
            <button type="button" class="song__close" aria-label="Close" @click="close">×</button>
          </header>

          <div class="song__body">
            <div v-if="failed" class="notice"><strong>Couldn't load this song.</strong><span>The recording may have been merged or removed.</span></div>

            <template v-else-if="doc">
              <div v-if="chips.length" class="song__chips">
                <span v-for="chip in chips" :key="chip" class="chip">{{ chip }}</span>
              </div>

              <LyricsPanel :recording-id="doc.id" />

              <section v-if="releases.length" class="song__section">
                <span class="section-label">Appears on</span>
                <ol class="line-list">
                  <li v-for="(release, index) in releases.slice(0, 12)" :key="index">
                    <span class="line-list__main">
                      <span class="line-list__title">{{ formatValue(release.title) || 'Release' }}</span>
                      <span class="line-list__sub">{{ [formatValue(release.country), titleCase(release.status)].filter(Boolean).join(' · ') }}</span>
                    </span>
                    <span class="line-list__meta">{{ formatDate(release.date) }}</span>
                  </li>
                </ol>
              </section>

              <ExternalIdsPanel :external-ids="doc.external_ids" />
              <LinksList :links="data.links" />
            </template>

            <div v-else-if="loading" class="song__loading"><span class="spinner" /></div>
          </div>

          <footer v-if="doc" class="song__foot">
            <NuxtLink :to="`/recordings/${doc.id}`" class="btn btn--sm btn--ghost" @click="close">Open full page ↗</NuxtLink>
          </footer>
        </aside>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.song { position: fixed; inset: 0; z-index: 120; display: flex; justify-content: flex-end; }
.song__scrim { position: absolute; inset: 0; background: rgba(6, 8, 10, 0.72); backdrop-filter: blur(4px); }
.song__drawer {
  position: relative;
  display: flex;
  flex-direction: column;
  width: min(460px, 94vw);
  height: 100%;
  border-left: 1px solid var(--line);
  background: var(--bg);
  box-shadow: -1rem 0 3rem rgba(0, 0, 0, 0.5);
}
.song__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  padding: 1.4rem 1.5rem 1rem;
  border-bottom: 1px solid var(--line);
}
.song__title { margin: 0.35rem 0 0; font-size: 1.3rem; font-weight: 600; line-height: 1.2; }
.song__artist { margin: 0.3rem 0 0; color: var(--muted); font-size: 0.82rem; }
.song__close {
  flex: none;
  width: 2rem; height: 2rem;
  border: 1px solid var(--line-strong); border-radius: 50%;
  background: none; color: var(--muted); font-size: 1.2rem; line-height: 1;
}
.song__close:hover { border-color: var(--gold); color: var(--gold); }

.song__body { flex: 1; overflow-y: auto; padding: 1.25rem 1.5rem; display: flex; flex-direction: column; gap: 1.1rem; }
.song__chips { display: flex; flex-wrap: wrap; gap: 0.45rem; }
.song__section .section-label { display: block; margin-bottom: 0.6rem; }
.song__loading { display: grid; place-items: center; padding: 3rem 0; }

.song__foot { padding: 1rem 1.5rem; border-top: 1px solid var(--line); }

.song-enter-active, .song-leave-active { transition: opacity 0.2s ease; }
.song-enter-active .song__drawer, .song-leave-active .song__drawer { transition: transform 0.24s ease; }
.song-enter-from, .song-leave-to { opacity: 0; }
.song-enter-from .song__drawer, .song-leave-to .song__drawer { transform: translateX(100%); }
</style>
