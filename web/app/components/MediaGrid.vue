<script setup lang="ts">
import type { CardShape } from '~/utils/kinds'

// Responsive grid. auto-fill (not auto-fit) keeps card size stable when a
// result set is small — empty tracks are preserved rather than stretched.
withDefaults(defineProps<{
  shape?: CardShape
  items?: any[]
}>(), { shape: 'poster', items: () => [] })
</script>

<template>
  <div class="media-grid" :class="`is-${shape}`">
    <slot>
      <MediaCard v-for="item in items" :key="item.id" :entity="item" :shape="shape" />
    </slot>
  </div>
</template>
