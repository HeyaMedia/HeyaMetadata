<script setup lang="ts">
import type { CardShape } from '~/utils/kinds'

// Horizontal shelf with fixed-width cards and prev/next pan controls. Two items
// stay normal card size — the track never stretches. Provide `items` for the
// default MediaCard rendering, or use the default slot for custom cards. Unlike
// MediaShelf, this keeps a "Browse all" link (curated homepage shelves point to
// the full domain rather than expanding in place).
const props = withDefaults(defineProps<{
  title: string
  kicker?: string
  shape?: CardShape
  items?: any[]
  browseTo?: string
  browseLabel?: string
}>(), { shape: 'poster', kicker: '', items: () => [], browseLabel: 'Browse all' })

const { rail, atStart, atEnd, pan, update } = useRailPan()
watch(() => props.items, () => nextTick(update), { immediate: true })
onMounted(() => nextTick(update))
</script>

<template>
  <section class="rail">
    <header class="rail__head">
      <div>
        <span v-if="kicker" class="section-label">{{ kicker }}</span>
        <h2>{{ title }}</h2>
      </div>
      <div class="rail-controls">
        <button type="button" class="rail-nav" :disabled="atStart" aria-label="Scroll left" @click="pan(-1)">‹</button>
        <button type="button" class="rail-nav" :disabled="atEnd" aria-label="Scroll right" @click="pan(1)">›</button>
        <NuxtLink v-if="browseTo" :to="browseTo" class="btn--link">{{ browseLabel }} ↗</NuxtLink>
      </div>
    </header>
    <div ref="rail" class="rail-track" :class="`is-${shape}`" @scroll.passive="update">
      <slot>
        <MediaCard v-for="item in items" :key="item.id" :entity="item" :shape="shape" />
      </slot>
    </div>
  </section>
</template>
