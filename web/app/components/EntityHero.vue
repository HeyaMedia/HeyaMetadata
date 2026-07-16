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

// Cinematic treatment: a scrimmed backdrop behind the hero and a logo image in
// place of the title when the providers supply them (mostly movie/tv/anime).
const backdropId = computed(() => props.images?.selections?.backdrop || props.images?.selections?.banner)
const logoId = computed(() => props.images?.selections?.logo || props.images?.selections?.clearlogo)

const metaChips = computed(() => {
  const chips: string[] = []
  const data = props.entity.data ?? {}
  if (props.entity.display?.year) chips.push(String(props.entity.display.year))
  const status = firstValue(data.status, data.classification?.status, data.release?.normalized_status)
  if (status) chips.push(titleCase(status))
  const runtime = formatRuntime(data.runtime_minutes ?? data.measurements?.runtime_minutes)
  if (runtime) chips.push(runtime)
  return chips
})

// Linked artist credits (album/recording/release) — wires music entities to
// their artist pages. Only the canonical artist_entity_id routes (artist_id is
// provider-scoped provenance); falls back to the display credit string.
const artistCredits = computed<{ name: string; to?: string }[]>(() => {
  const credits = props.entity.data?.artist_credits
  if (Array.isArray(credits) && credits.length) {
    return credits
      .map((credit: any) => ({
        name: formatValue(credit.artist_name ?? credit.name),
        to: credit.artist_entity_id ? entityPath({ id: credit.artist_entity_id, kind: 'artist' }) : undefined,
      }))
      .filter(credit => credit.name)
  }
  const display = formatValue(props.entity.display?.artist_credit)
  return display ? [{ name: display }] : []
})

// Kind-aware headline stats — the main lever that makes each hero feel distinct.
const statStrip = computed<{ value: string; label: string }[]>(() => {
  const data: any = props.entity.data ?? {}
  const out: { value: string; label: string }[] = []
  const push = (value: unknown, label: string) => {
    const text = typeof value === 'number' ? formatCount(value) : formatValue(value)
    if (text) out.push({ value: text, label })
  }
  switch (props.entity.kind) {
    case 'movie': {
      const top = [...(data.ratings ?? [])].sort((a, b) => (b.votes ?? 0) - (a.votes ?? 0))[0]
      if (top) out.push({ value: ratingValue(top), label: formatKey(top.system || 'rating') })
      break
    }
    case 'tv_show':
    case 'anime':
      push(Array.isArray(data.seasons) ? data.seasons.length : '', 'Seasons')
      push(data.episode_count, 'Episodes')
      break
    case 'release_group':
      push(Array.isArray(data.tracks) ? data.tracks.length : '', 'Tracks')
      push(Array.isArray(data.editions) ? data.editions.length : '', 'Releases')
      break
    case 'recording':
      push(formatDuration(data.duration_ms), 'Duration')
      break
    case 'artist':
      for (const metric of (data.metrics ?? []).slice(0, 2)) push(metric.value, formatKey(metric.name))
      break
    case 'book_work':
      push(Array.isArray(data.editions) ? data.editions.length : '', 'Editions')
      break
    case 'manga':
      push(data.volume_count, 'Volumes')
      push(data.chapter_count, 'Chapters')
      break
    case 'manga_volume':
    case 'comic_volume':
      push(Array.isArray(data.editions) ? data.editions.length : '', 'Editions')
      push(data.page_count, 'Pages')
      break
  }
  return out
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
  <section class="hero" :class="{ 'has-backdrop': backdropId }">
    <div v-if="backdropId" class="hero__backdrop" aria-hidden="true">
      <MetadataImage :image-id="backdropId" variant="hero" decorative />
      <span class="hero__backdrop-scrim" />
    </div>
    <div v-else class="hero__glow" aria-hidden="true" />
    <div class="hero__art" :class="`hero__art--${shape}`">
      <MetadataImage :image-id="primaryImage" :alt="title" variant="hero" />
    </div>

    <div class="hero__body">
      <p class="hero__kicker">
        <span>{{ kindText }}</span>
        <template v-if="freshness"><i aria-hidden="true" />{{ freshness }}</template>
      </p>
      <h1 v-if="logoId" class="hero__logo">
        <MetadataImage :image-id="logoId" :alt="title" variant="hero" />
      </h1>
      <h1 v-else class="editorial hero__title">{{ title }}</h1>
      <p v-if="originalTitle" class="hero__original">{{ originalTitle }}</p>
      <p v-if="artistCredits.length" class="hero__artists">
        by <template v-for="(credit, index) in artistCredits" :key="index"><NuxtLink v-if="credit.to" :to="credit.to" class="hero__artist-link">{{ credit.name }}</NuxtLink><span v-else>{{ credit.name }}</span><template v-if="index < artistCredits.length - 1">, </template></template>
      </p>

      <p v-if="description" class="hero__description" :class="{ 'is-clamped': longDescription && !expanded }">
        {{ description }}
      </p>
      <button v-if="longDescription" type="button" class="btn--link hero__expand" @click="expanded = !expanded">
        {{ expanded ? 'Show less' : 'Read more' }}
      </button>

      <div v-if="metaChips.length" class="chip-row hero__meta">
        <span v-for="chip in metaChips" :key="chip" class="chip">{{ chip }}</span>
      </div>

      <dl v-if="statStrip.length" class="hero__stats">
        <div v-for="stat in statStrip" :key="stat.label">
          <dt>{{ stat.value }}</dt>
          <dd>{{ stat.label }}</dd>
        </div>
      </dl>

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
.hero.has-backdrop { padding-top: clamp(2rem, 5vw, 3.5rem); }
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
.hero__backdrop {
  position: absolute;
  z-index: 0;
  inset: -1.25rem 0 auto 0;
  height: calc(100% + 2.5rem);
  overflow: hidden;
  border-radius: var(--radius);
}
.hero__backdrop :deep(.metadata-image) { width: 100%; height: 100%; }
.hero__backdrop-scrim {
  position: absolute;
  inset: 0;
  background:
    linear-gradient(to top, var(--bg) 2%, rgba(11, 14, 16, 0.6) 55%, rgba(11, 14, 16, 0.32) 100%),
    linear-gradient(90deg, var(--bg) 0%, rgba(11, 14, 16, 0.2) 46%, rgba(11, 14, 16, 0.05) 100%);
}
.hero__art, .hero__body { position: relative; z-index: 1; }
.hero__logo {
  width: min(30rem, 100%);
  height: clamp(3.25rem, 7vw, 5rem);
  margin: 0.35rem 0 0.5rem;
}
.hero__logo :deep(.metadata-image) { width: 100%; height: 100%; background: transparent; }
.hero__logo :deep(img) { object-fit: contain; object-position: left center; }
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
.hero__original { margin: 0 0 0.4rem; color: #758087; font-size: 0.92rem; }
.hero__artists { margin: 0.1rem 0 0; color: var(--muted); font-size: 0.95rem; }
.hero__artist-link { color: var(--gold); }
.hero__artist-link:hover { text-decoration: underline; }
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
.hero__stats { display: flex; flex-wrap: wrap; gap: 2rem; margin: 1.25rem 0 0; }
.hero__stats div { display: flex; flex-direction: column; }
.hero__stats dt { font-family: var(--font-mono); font-size: 1.35rem; line-height: 1.1; }
.hero__stats dd { margin: 0.2rem 0 0; color: var(--muted-2); font-size: 0.62rem; letter-spacing: 0.06em; text-transform: uppercase; }
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
