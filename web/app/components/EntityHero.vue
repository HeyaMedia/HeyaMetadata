<script setup lang="ts">
import type { CardShape } from '~/utils/kinds'
import type { EntityDocument, ImagesResponse } from '~/utils/types'

// Compressed cinematic hero: poster + adjacent title block. Reduced top
// padding, poster ~220–280px (not a half-page column), clamped description with
// an explicit expand control.
const props = defineProps<{
  entity: EntityDocument
  images?: ImagesResponse | null
  refreshing?: boolean
}>()

const emit = defineEmits<{ refresh: [] }>()

const shape = computed<CardShape>(() => cardShape(props.entity.kind))
const title = computed(() => entityTitle(props.entity))
const kindText = computed(() => kindLabel(props.entity.kind))
const freshness = computed(() => formatValue(props.entity.freshness?.state))

const originalTitle = computed(() => {
  const original = formatValue(props.entity.display?.original_title)
  return original && original !== title.value ? original : ''
})

const description = computed(() =>
  formatValue(
    props.entity.presentation?.description
    || props.entity.data?.description
    || props.entity.data?.overview,
  ),
)
const longDescription = computed(() => description.value.length > 280)
const expanded = ref(false)

const primaryImage = computed(() => {
  const selections = props.images?.selections ?? {}
  const presentation = props.entity.presentation?.images ?? {}
  return (
    selections.poster || selections.cover || selections.profile || selections.primary
    || presentation.poster || presentation.cover || presentation.profile
    || props.entity.display?.image_id
  )
})

const metaChips = computed(() => {
  const chips: string[] = []
  const data = props.entity.data ?? {}
  if (props.entity.display?.year) chips.push(String(props.entity.display.year))
  const status = firstValue(data.status, data.classification?.status, data.release?.normalized_status)
  if (status) chips.push(titleCase(status))
  const runtime = formatRuntime(data.runtime_minutes ?? data.measurements?.runtime_minutes)
  if (runtime) chips.push(runtime)
  const artistCredit = formatValue(props.entity.display?.artist_credit)
  if (artistCredit) chips.unshift(artistCredit)
  return chips
})

const copied = ref(false)
async function copyId() {
  try {
    await navigator.clipboard.writeText(props.entity.id)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1400)
  } catch {
    /* clipboard unavailable */
  }
}
</script>

<template>
  <section class="hero">
    <div class="hero__glow" aria-hidden="true" />
    <div class="hero__art" :class="`hero__art--${shape}`">
      <MetadataImage :image-id="primaryImage" :alt="title" variant="hero" />
    </div>

    <div class="hero__body">
      <p class="hero__kicker">
        <span>{{ kindText }}</span>
        <template v-if="freshness"><i aria-hidden="true" />{{ freshness }}</template>
      </p>
      <h1 class="editorial hero__title">{{ title }}</h1>
      <p v-if="originalTitle" class="hero__original">{{ originalTitle }}</p>

      <p v-if="description" class="hero__description" :class="{ 'is-clamped': longDescription && !expanded }">
        {{ description }}
      </p>
      <button v-if="longDescription" type="button" class="btn--link hero__expand" @click="expanded = !expanded">
        {{ expanded ? 'Show less' : 'Read more' }}
      </button>

      <div v-if="metaChips.length" class="chip-row hero__meta">
        <span v-for="chip in metaChips" :key="chip" class="chip">{{ chip }}</span>
      </div>

      <div class="hero__actions">
        <button type="button" class="btn btn--gold" :disabled="refreshing" @click="emit('refresh')">
          {{ refreshing ? 'Refreshing…' : 'Refresh providers' }}
        </button>
        <button type="button" class="hero__id" :title="entity.id" @click="copyId">
          <code>{{ entity.id }}</code>
          <b>{{ copied ? 'Copied ✓' : 'Copy ID' }}</b>
        </button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.hero {
  position: relative;
  display: grid;
  grid-template-columns: minmax(11rem, 16rem) 1fr;
  gap: clamp(1.75rem, 4vw, 3.5rem);
  align-items: start;
  padding-top: clamp(1.5rem, 3vw, 2.5rem);
}
.hero__glow {
  position: absolute;
  z-index: -1;
  top: 0;
  left: 4%;
  width: 26rem;
  height: 26rem;
  border-radius: 50%;
  background: rgba(138, 121, 60, 0.09);
  filter: blur(80px);
}
.hero__art {
  width: 100%;
  overflow: hidden;
  border: 1px solid var(--line-strong);
  border-radius: var(--radius);
  box-shadow: 0 1.5rem 4rem rgba(0, 0, 0, 0.45);
}
.hero__art--poster { aspect-ratio: 2 / 3; }
.hero__art--portrait { aspect-ratio: 3 / 4; }
.hero__art--square { aspect-ratio: 1 / 1; }
.hero__art--landscape { aspect-ratio: 16 / 9; }

.hero__body { min-width: 0; max-width: 56rem; }
.hero__kicker {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  margin: 0;
  color: #8b9697;
  font-family: var(--font-mono);
  font-size: 0.64rem;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.hero__kicker span { color: var(--gold); }
.hero__kicker i { width: 0.25rem; height: 0.25rem; border-radius: 50%; background: #536064; }
.hero__title { margin: 0.7rem 0 0.4rem; font-size: clamp(2rem, 4.2vw, 3.6rem); }
.hero__original { margin: 0 0 0.6rem; color: #758087; font-size: 0.92rem; }
.hero__description {
  max-width: 52rem;
  margin: 0.75rem 0 0;
  color: #a0aaa7;
  font-size: 0.86rem;
  line-height: 1.72;
}
.hero__description.is-clamped {
  display: -webkit-box;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 4;
}
.hero__expand { margin-top: 0.5rem; color: var(--gold); }
.hero__meta { margin-top: 1rem; }
.hero__actions { display: flex; flex-wrap: wrap; gap: 0.65rem; margin-top: 1.35rem; }
.hero__id {
  display: flex;
  align-items: stretch;
  overflow: hidden;
  max-width: 24rem;
  border: 1px solid var(--line-strong);
  border-radius: var(--radius-sm);
  background: var(--panel-2);
}
.hero__id code {
  overflow: hidden;
  padding: 0.7rem 0.8rem;
  color: #748086;
  font-family: var(--font-mono);
  font-size: 0.6rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.hero__id b {
  padding: 0.7rem 0.8rem;
  border-left: 1px solid var(--line-strong);
  color: #b9c0bd;
  font-size: 0.64rem;
  font-weight: 500;
  white-space: nowrap;
}
.hero__id:hover b { color: var(--gold); }

@media (max-width: 720px) {
  .hero { grid-template-columns: 1fr; }
  .hero__art { width: min(15rem, 62vw); }
}
</style>
