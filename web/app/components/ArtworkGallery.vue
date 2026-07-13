<script setup lang="ts">
import type { ImageCandidate, ImagesResponse } from '~/utils/types'

// Language-aware artwork inspector, grouped by class, each group a MediaShelf
// (rail + pan + expand) so a 300-image gallery never renders/fetches everything
// up front. Selected artwork is outlined in gold; clicking a tile opens a
// full-size lightbox.
const props = defineProps<{ images?: ImagesResponse | null }>()

const CLASS_ORDER = ['poster', 'cover', 'primary', 'backdrop', 'banner', 'thumb', 'profile', 'logo', 'clearlogo', 'clearart', 'disc', 'commons']
const CONTAIN = new Set(['logo', 'clearlogo', 'clearart', 'disc'])
const WIDE = new Set(['backdrop', 'banner', 'landscape', 'logo', 'clearlogo', 'clearart', 'thumb', 'commons'])

const groups = computed(() => {
  const map = new Map<string, ImageCandidate[]>()
  for (const image of props.images?.results ?? []) {
    const key = image.class || 'other'
    if (!map.has(key)) map.set(key, [])
    map.get(key)!.push(image)
  }
  return [...map.entries()]
    .sort((a, b) => {
      const ai = CLASS_ORDER.indexOf(a[0])
      const bi = CLASS_ORDER.indexOf(b[0])
      return (ai === -1 ? 99 : ai) - (bi === -1 ? 99 : bi)
    })
    .map(([cls, items]) => ({ cls, items, wide: WIDE.has(cls), contain: CONTAIN.has(cls) }))
})

const total = computed(() => props.images?.results?.length ?? 0)

function dims(image: ImageCandidate): string {
  return image.width && image.height ? `${image.width}×${image.height}` : image.id.slice(0, 8)
}

// Lightbox: view a tile full-size. Custom overlay (not a native dialog).
const active = ref<ImageCandidate | null>(null)
function open(image: ImageCandidate) { active.value = image }
function close() { active.value = null }
function onKey(event: KeyboardEvent) { if (event.key === 'Escape') close() }
watch(active, value => { if (import.meta.client) document.body.style.overflow = value ? 'hidden' : '' })
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  if (import.meta.client) document.body.style.overflow = ''
})
</script>

<template>
  <div class="artwork">
    <header class="artwork__intro">
      <span class="section-label">Language-aware selection</span>
      <h2>{{ total }} image {{ total === 1 ? 'candidate' : 'candidates' }}</h2>
      <p>Selected artwork is outlined in gold. Each asset materializes lazily and is cached by Heya.</p>
    </header>

    <MediaShelf
      v-for="group in groups"
      :key="group.cls"
      :title="formatKey(group.cls)"
      kicker="Artwork"
      :items="group.items"
      :shape="group.wide ? 'landscape' : 'poster'"
      :item-key="img => img.id"
    >
      <template #default="{ item: image }">
        <figure class="art-tile" :class="{ 'is-selected': image.selected, 'is-wide': group.wide, 'is-contain': group.contain }">
          <button type="button" class="art-tile__art" @click="open(image)">
            <MetadataImage :image-id="image.id" :alt="`${formatKey(image.class || '')} from ${image.provider}`" variant="card" />
          </button>
          <figcaption>
            <strong>{{ image.provider }}</strong>
            <span><template v-if="image.language">{{ image.language }} · </template>{{ dims(image) }}</span>
            <em v-if="image.selected">Selected · {{ formatKey(image.selection_reason || 'chosen') }}</em>
          </figcaption>
        </figure>
      </template>
    </MediaShelf>

    <EmptyState
      v-if="!total"
      title="No artwork candidates."
      message="This is likely a provider coverage gap worth investigating."
    />

    <Teleport to="body">
      <div v-if="active" class="lightbox" role="dialog" aria-modal="true" @click.self="close">
        <button type="button" class="lightbox__close" aria-label="Close" @click="close">×</button>
        <div class="lightbox__stage">
          <MetadataImage :key="active.id" :image-id="active.id" :alt="`${formatKey(active.class || '')} from ${active.provider}`" variant="hero" />
        </div>
        <div class="lightbox__meta">
          <strong>{{ formatKey(active.class || 'image') }}</strong>
          <span>{{ active.provider }}<template v-if="active.language"> · {{ active.language }}</template> · {{ dims(active) }}</span>
          <em v-if="active.selected">Selected · {{ formatKey(active.selection_reason || 'chosen') }}</em>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.artwork__intro { margin-bottom: 0.5rem; }
.artwork__intro h2 { margin: 0.4rem 0 0.3rem; font-size: 1.35rem; font-weight: 500; }
.artwork__intro p { max-width: 40rem; margin: 0; color: var(--muted); font-size: 0.78rem; }

.art-tile { margin: 0; overflow: hidden; border: 1px solid var(--line-soft); border-radius: var(--radius); background: var(--panel); }
.art-tile.is-selected { border-color: var(--gold); box-shadow: 0 0 0 1px var(--gold); }
.art-tile__art { display: block; width: 100%; aspect-ratio: 2 / 3; padding: 0; border: 0; background: #12171c; cursor: zoom-in; }
.art-tile.is-wide .art-tile__art { aspect-ratio: 16 / 9; }
.art-tile.is-contain .art-tile__art :deep(.metadata-image) { background: #0c1013; }
.art-tile.is-contain .art-tile__art :deep(img) { object-fit: contain; }
.art-tile__art:hover :deep(img) { transform: scale(1.03); }
.art-tile__art :deep(img) { transition: transform 0.25s ease; }
figcaption { display: flex; flex-direction: column; gap: 0.12rem; padding: 0.55rem 0.65rem 0.65rem; }
figcaption strong { font-size: 0.72rem; text-transform: capitalize; }
figcaption span { color: var(--muted-2); font-size: 0.62rem; }
figcaption em { color: var(--gold); font-size: 0.58rem; font-style: normal; }

.lightbox {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: grid;
  place-items: center;
  gap: 1rem;
  padding: clamp(1.5rem, 5vw, 4rem);
  background: rgba(6, 8, 10, 0.9);
  backdrop-filter: blur(6px);
}
.lightbox__stage { width: min(92vw, 1100px); height: min(78vh, 760px); }
.lightbox__stage :deep(.metadata-image) { width: 100%; height: 100%; background: transparent; }
.lightbox__stage :deep(img) { object-fit: contain; }
.lightbox__meta { display: flex; flex-wrap: wrap; align-items: baseline; gap: 0.3rem 0.8rem; color: var(--muted); font-size: 0.72rem; }
.lightbox__meta strong { color: var(--text); text-transform: capitalize; }
.lightbox__meta em { color: var(--gold); font-style: normal; }
.lightbox__close {
  position: absolute;
  top: 1.25rem;
  right: 1.5rem;
  width: 2.4rem;
  height: 2.4rem;
  border: 1px solid var(--line-strong);
  border-radius: 50%;
  background: rgba(17, 22, 25, 0.8);
  color: var(--text);
  font-size: 1.3rem;
  line-height: 1;
}
.lightbox__close:hover { border-color: var(--gold); color: var(--gold); }
</style>
