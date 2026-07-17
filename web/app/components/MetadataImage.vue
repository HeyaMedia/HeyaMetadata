<script setup lang="ts">
// Lazy, self-materializing image. It only fetches when near the viewport, asks
// for an appropriately-sized WebP variant, follows the server's 202 async
// materialization with bounded backoff, falls back to the original route, and
// revokes object URLs on replacement/unmount. It never eagerly materializes an
// off-screen gallery.
const props = withDefaults(defineProps<{
  imageId?: string
  alt?: string
  /** picks the requested WebP width */
  variant?: 'thumb' | 'card' | 'hero' | 'original'
  /** decorative backdrops render with empty alt and are hidden from a11y tree */
  decorative?: boolean
}>(), { alt: '', variant: 'card', decorative: false })

const WIDTHS: Record<string, number> = { thumb: 320, card: 640, hero: 1280, original: 960 }

const source = ref('')
const state = ref<'idle' | 'loading' | 'ready' | 'missing'>('idle')
const container = ref<HTMLElement | null>(null)
const visible = ref(false)
let objectURL = ''
let generation = 0
let observer: IntersectionObserver | undefined

function release() {
  if (objectURL) URL.revokeObjectURL(objectURL)
  objectURL = ''
  source.value = ''
}

function sleep(ms: number) {
  return new Promise(resolve => setTimeout(resolve, ms))
}

async function loadImage(id?: string) {
  const current = ++generation
  release()
  if (!id) {
    state.value = 'missing'
    return
  }
  state.value = 'loading'
  const width = WIDTHS[props.variant] ?? WIDTHS.card
  const variantURL = `/api/v2/images/${id}/variants/webp/${width}`
  const originalURL = `/api/v2/images/${id}`
  for (let attempt = 0; attempt < 12 && current === generation; attempt++) {
    let response = await fetch(variantURL)
    if (response.status === 404) response = await fetch(originalURL)
    if (response.ok && response.status === 200 && response.headers.get('content-type')?.startsWith('image/')) {
      if (current !== generation) return
      objectURL = URL.createObjectURL(await response.blob())
      source.value = objectURL
      state.value = 'ready'
      return
    }
    if (response.status !== 202) break
    // The 202 carries Retry-After with the server's expected materialization
    // pace; fall back to local backoff when it is absent or unparsable.
    const retryAfter = Number(response.headers.get('retry-after'))
    await sleep(retryAfter > 0 ? Math.min(retryAfter * 1000, 3000) : Math.min(250 + attempt * 150, 1500))
  }
  if (current === generation) state.value = 'missing'
}

watch(() => [props.imageId, props.variant], () => {
  if (visible.value) loadImage(props.imageId)
  else { generation++; release(); state.value = props.imageId ? 'idle' : 'missing' }
})
watch(visible, value => { if (value) loadImage(props.imageId) })

onMounted(() => {
  if (!('IntersectionObserver' in window)) { visible.value = true; return }
  observer = new IntersectionObserver(entries => {
    if (entries.some(entry => entry.isIntersecting)) {
      visible.value = true
      observer?.disconnect()
    }
  }, { rootMargin: '300px' })
  if (container.value) observer.observe(container.value)
})
onBeforeUnmount(() => { generation++; observer?.disconnect(); release() })
</script>

<template>
  <div ref="container" class="metadata-image" :class="[`is-${state}`]">
    <img v-if="source" :src="source" :alt="decorative ? '' : alt" loading="lazy">
    <div v-else class="image-placeholder" :aria-hidden="true">
      <span v-if="state === 'loading'" class="spinner" />
      <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.4">
        <rect x="3" y="4" width="18" height="16" rx="2" />
        <circle cx="9" cy="10" r="2" />
        <path d="m4 17 4.8-4.8a2 2 0 0 1 2.8 0L14 14.6l1.2-1.2a2 2 0 0 1 2.8 0l2 2" />
      </svg>
    </div>
  </div>
</template>

<style scoped>
.metadata-image {
  position: relative;
  width: 100%;
  height: 100%;
  overflow: hidden;
  background: #12171c;
  color: #4b555d;
}
.metadata-image img { display: block; width: 100%; height: 100%; object-fit: cover; }
.image-placeholder {
  display: grid;
  width: 100%;
  height: 100%;
  place-items: center;
  background: linear-gradient(150deg, #161c21, #0f1317);
}
.image-placeholder svg { width: 1.9rem; opacity: 0.45; }
.spinner {
  width: 1.3rem;
  height: 1.3rem;
  border: 2px solid #344049;
  border-top-color: var(--gold);
  border-radius: 50%;
  animation: heya-spin 0.8s linear infinite;
}
</style>
