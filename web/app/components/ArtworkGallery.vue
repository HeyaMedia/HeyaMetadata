<script setup lang="ts">
import type { ImageCandidate, ImagesResponse } from '~/utils/types'

// Language-aware artwork inspector, grouped by class. Every tile lazily
// materializes via MetadataImage's IntersectionObserver, so opening the tab does
// not eagerly fetch the whole gallery. Selected artwork is outlined in gold.
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
    .map(([cls, items]) => ({ cls, items }))
})

const total = computed(() => props.images?.results?.length ?? 0)

function dims(image: ImageCandidate): string {
  return image.width && image.height ? `${image.width}×${image.height}` : image.id.slice(0, 8)
}

// Lightbox: view an artwork tile full-size. Custom overlay (not a native
// dialog), closed by Esc, the close button, or a backdrop click.
const active = ref<ImageCandidate | null>(null)
function open(image: ImageCandidate) { active.value = image }
function close() { active.value = null }

function onKey(event: KeyboardEvent) { if (event.key === 'Escape') close() }
watch(active, value => {
  if (import.meta.client) document.body.style.overflow = value ? 'hidden' : ''
})
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

    <section v-for="group in groups" :key="group.cls" class="artwork__group">
      <h3 class="artwork__group-title">{{ formatKey(group.cls) }} <small>{{ group.items.length }}</small></h3>
      <div class="artwork__grid" :class="{ 'is-wide': WIDE.has(group.cls) }">
        <figure v-for="image in group.items" :key="image.id" :class="{ 'is-selected': image.selected }">
          <button type="button" class="artwork__art" :class="{ 'is-contain': CONTAIN.has(group.cls) }" @click="open(image)">
            <MetadataImage :image-id="image.id" :alt="`${formatKey(image.class || '')} from ${image.provider}`" variant="card" />
          </button>
          <figcaption>
            <strong>{{ image.provider }}</strong>
            <span>
              <template v-if="image.language">{{ image.language }} · </template>{{ dims(image) }}
            </span>
            <em v-if="image.selected">Selected · {{ formatKey(image.selection_reason || 'chosen') }}</em>
          </figcaption>
        </figure>
      </div>
    </section>

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
.artwork__intro { margin-bottom: 1.75rem; }
.artwork__intro h2 { margin: 0.4rem 0 0.3rem; font-size: 1.35rem; font-weight: 500; }
.artwork__intro p { max-width: 40rem; margin: 0; color: var(--muted); font-size: 0.78rem; }
.artwork__group { margin-bottom: 2rem; }
.artwork__group-title {
  display: flex;
  align-items: baseline;
  gap: 0.5rem;
  margin: 0 0 0.9rem;
  font-size: 0.82rem;
  font-weight: 500;
}
.artwork__group-title small { color: var(--muted-2); font-family: var(--font-mono); font-size: 0.66rem; }
.artwork__grid {
  display: grid;
  gap: 1rem;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
}
.artwork__grid.is-wide { grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); }
figure {
  margin: 0;
  overflow: hidden;
  border: 1px solid var(--line-soft);
  border-radius: var(--radius);
  background: var(--panel);
}
figure.is-selected { border-color: var(--gold); box-shadow: 0 0 0 1px var(--gold); }
.artwork__art { display: block; aspect-ratio: 2 / 3; width: 100%; padding: 0; border: 0; background: #12171c; cursor: zoom-in; }
.artwork__art:hover :deep(img) { transform: scale(1.03); }
.artwork__art :deep(img) { transition: transform 0.25s ease; }
.is-wide .artwork__art { aspect-ratio: 16 / 9; }
.artwork__art.is-contain :deep(.metadata-image) { background: #0c1013; }
.artwork__art.is-contain :deep(img) { object-fit: contain; }
figcaption { display: flex; flex-direction: column; gap: 0.15rem; padding: 0.7rem 0.75rem 0.8rem; }
figcaption strong { font-size: 0.74rem; text-transform: capitalize; }
figcaption span { color: var(--muted-2); font-size: 0.64rem; }
figcaption em { color: var(--gold); font-size: 0.6rem; font-style: normal; }

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
.lightbox__stage {
  width: min(92vw, 1100px);
  height: min(78vh, 760px);
}
.lightbox__stage :deep(.metadata-image) { width: 100%; height: 100%; background: transparent; }
.lightbox__stage :deep(img) { object-fit: contain; }
.lightbox__meta {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 0.3rem 0.8rem;
  color: var(--muted);
  font-size: 0.72rem;
}
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
