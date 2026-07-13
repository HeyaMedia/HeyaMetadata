<script setup lang="ts">
import type { CardShape } from '~/utils/kinds'

// Horizontal shelf with fixed-width cards. Two items stay normal card size —
// the track never stretches to fill the viewport. Provide `items` for the
// default MediaCard rendering, or use the default slot for custom cards.
withDefaults(defineProps<{
  title: string
  kicker?: string
  shape?: CardShape
  items?: any[]
  browseTo?: string
  browseLabel?: string
}>(), { shape: 'poster', kicker: '', items: () => [], browseLabel: 'Browse all' })
</script>

<template>
  <section class="rail">
    <header class="rail__head">
      <div>
        <span v-if="kicker" class="section-label">{{ kicker }}</span>
        <h2>{{ title }}</h2>
      </div>
      <NuxtLink v-if="browseTo" :to="browseTo" class="btn--link">{{ browseLabel }} ↗</NuxtLink>
    </header>
    <div class="rail-track" :class="`is-${shape}`">
      <slot>
        <MediaCard v-for="item in items" :key="item.id" :entity="item" :shape="shape" />
      </slot>
    </div>
  </section>
</template>
