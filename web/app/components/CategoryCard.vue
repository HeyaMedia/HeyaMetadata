<script setup lang="ts">
import type { CardShape } from '~/utils/kinds'

// A browse landing tile: a collage of representative artwork from a domain with
// its label and live count. Purely a navigation affordance into
// /browse?kind=<kind> — the collage is decorative, so images render with empty
// alt and the whole card carries one accessible label.
const props = defineProps<{
  label: string
  kind: string
  count: number
  noun: string
  shape: CardShape
  /** representative image ids (up to a handful); may contain undefined */
  samples: Array<string | undefined>
}>()

const to = computed(() => `/browse?kind=${encodeURIComponent(props.kind)}`)
// Up to four real thumbnails; when a domain has fewer we let the strip shrink
// rather than pad it with empty placeholders.
const tiles = computed(() => props.samples.filter(Boolean).slice(0, 4) as string[])
const aria = computed(() => `${props.label} — ${formatCount(props.count)} ${props.noun}`)
</script>

<template>
  <NuxtLink :to="to" class="category-card" :aria-label="aria">
    <span class="category-card__art" :class="`is-${shape}`">
      <template v-if="tiles.length">
        <span v-for="id in tiles" :key="id" class="category-card__tile">
          <MetadataImage :image-id="id" variant="thumb" decorative />
        </span>
      </template>
      <span v-else class="category-card__empty" aria-hidden="true">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.3">
          <rect x="3" y="4" width="18" height="16" rx="2" />
          <circle cx="9" cy="10" r="2" />
          <path d="m4 17 4.8-4.8a2 2 0 0 1 2.8 0L14 14.6l1.2-1.2a2 2 0 0 1 2.8 0l2 2" />
        </svg>
      </span>
      <span class="category-card__scrim" />
    </span>
    <span class="category-card__body">
      <strong class="category-card__title">{{ label }}</strong>
      <span class="category-card__meta">{{ formatCount(count) }} {{ noun }}</span>
      <span class="category-card__go" aria-hidden="true">→</span>
    </span>
  </NuxtLink>
</template>

<style scoped>
.category-card {
  position: relative;
  display: block;
  overflow: hidden;
  border: 1px solid var(--line-soft);
  border-radius: var(--radius);
  background: var(--panel);
  transition: transform 0.18s ease, border-color 0.18s ease;
}
.category-card:hover { transform: translateY(-3px); border-color: #5a5236; }
.category-card:focus-visible { outline-offset: 3px; }

.category-card__art {
  position: relative;
  display: flex;
  aspect-ratio: 16 / 8;
  overflow: hidden;
  background: #12171c;
  gap: 1px;
}
.category-card__tile { flex: 1 1 0; min-width: 0; }
.category-card__empty {
  display: grid;
  flex: 1;
  place-items: center;
  color: #3d474e;
  background: linear-gradient(150deg, #161c21, #0f1317);
}
.category-card__empty svg { width: 2rem; opacity: 0.5; }
.category-card__scrim {
  position: absolute;
  inset: 0;
  background: linear-gradient(to top, rgba(8, 11, 13, 0.94) 4%, rgba(8, 11, 13, 0.35) 45%, transparent 78%);
}

.category-card__body {
  position: absolute;
  inset-inline: 0;
  bottom: 0;
  display: grid;
  grid-template-columns: 1fr auto;
  align-items: end;
  gap: 0.15rem 0.5rem;
  padding: 0.85rem 0.95rem;
}
.category-card__title {
  grid-column: 1;
  font-size: 1.05rem;
  font-weight: 600;
  line-height: 1.1;
}
.category-card__meta {
  grid-column: 1;
  color: var(--gold);
  font-family: var(--font-mono);
  font-size: 0.66rem;
  letter-spacing: 0.02em;
}
.category-card__go {
  grid-column: 2;
  grid-row: 1 / span 2;
  align-self: center;
  color: var(--muted-2);
  font-size: 1rem;
  transition: transform 0.18s ease, color 0.18s ease;
}
.category-card:hover .category-card__go { transform: translateX(3px); color: var(--gold); }
</style>
