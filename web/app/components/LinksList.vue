<script setup lang="ts">
// External links. Normalizes the two shapes in the data: {kind,value} (movie)
// and {type,url} (artist/recording).
const props = defineProps<{ links?: any[] }>()

const links = computed(() =>
  (props.links ?? [])
    .map(link => ({
      label: titleCase(link.type ?? link.kind ?? 'link'),
      url: formatValue(link.url ?? link.value),
    }))
    .filter(link => /^https?:\/\//i.test(link.url)),
)
</script>

<template>
  <OverviewPanel v-if="links.length" title="Links" kicker="Off-platform">
    <div class="links">
      <a v-for="(link, index) in links" :key="index" :href="link.url" target="_blank" rel="noopener noreferrer" class="chip chip--accent">
        {{ link.label }} ↗
      </a>
    </div>
  </OverviewPanel>
</template>

<style scoped>
.links { display: flex; flex-wrap: wrap; gap: 0.5rem; }
.links a { text-decoration: none; }
</style>
