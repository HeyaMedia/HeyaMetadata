<script setup lang="ts">
import type { CardShape } from '~/utils/kinds'

// Placeholder cards that match final card geometry to prevent layout shift.
withDefaults(defineProps<{
  count?: number
  shape?: CardShape
  layout?: 'grid' | 'rail'
}>(), { count: 6, shape: 'poster', layout: 'grid' })
</script>

<template>
  <div :class="layout === 'rail' ? ['rail-track', `is-${shape}`] : ['media-grid', `is-${shape}`]" aria-hidden="true">
    <div v-for="n in count" :key="n" class="skeleton-card" :class="`card--${shape}`">
      <span class="skeleton-card__art skeleton" />
      <span class="skeleton-card__body">
        <span class="skeleton skeleton-card__line skeleton-card__line--kind" />
        <span class="skeleton skeleton-card__line skeleton-card__line--title" />
      </span>
    </div>
  </div>
</template>

<style scoped>
.skeleton-card {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--line-soft);
  border-radius: var(--radius);
  background: var(--panel);
}
.skeleton-card__art { display: block; width: 100%; }
.card--poster .skeleton-card__art { aspect-ratio: 2 / 3; }
.card--portrait .skeleton-card__art { aspect-ratio: 3 / 4; }
.card--square .skeleton-card__art { aspect-ratio: 1 / 1; }
.card--landscape .skeleton-card__art { aspect-ratio: 16 / 9; }
.skeleton-card__body { display: flex; flex-direction: column; gap: 0.45rem; padding: 0.7rem 0.75rem 0.9rem; }
.skeleton-card__line { height: 0.6rem; border-radius: 3px; }
.skeleton-card__line--kind { width: 40%; }
.skeleton-card__line--title { width: 75%; height: 0.75rem; }

.skeleton {
  background: linear-gradient(100deg, #141a1e 30%, #1c242a 50%, #141a1e 70%);
  background-size: 200% 100%;
  animation: heya-shimmer 1.3s linear infinite;
}
</style>
