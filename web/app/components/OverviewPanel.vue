<script setup lang="ts">
withDefaults(defineProps<{
  title?: string
  kicker?: string
  full?: boolean
}>(), { title: '', kicker: '', full: false })
</script>

<template>
  <section class="panel overview-panel" :class="{ 'is-full': full }">
    <header v-if="title || kicker">
      <span v-if="kicker" class="section-label">{{ kicker }}</span>
      <h2 v-if="title">{{ title }}</h2>
    </header>
    <slot />
  </section>
</template>

<style scoped>
.overview-panel { min-width: 0; }
.overview-panel.is-full { grid-column: 1 / -1; }
</style>
