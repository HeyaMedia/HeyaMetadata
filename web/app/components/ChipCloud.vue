<script setup lang="ts">
// A capped, expandable cloud of short string values (subjects, genres, tags).
// Deduped and empty-filtered; collapses to `cap` chips with a "show all".
const props = withDefaults(defineProps<{
  title: string
  kicker?: string
  items?: unknown[]
  cap?: number
  full?: boolean
}>(), { kicker: '', items: () => [], cap: 28, full: false })

const all = computed(() => {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of props.items ?? []) {
    const value = formatValue(raw)
    const key = value.toLowerCase()
    if (!value || seen.has(key)) continue
    seen.add(key)
    out.push(value)
  }
  return out
})

const showAll = ref(false)
const shown = computed(() => (showAll.value ? all.value : all.value.slice(0, props.cap)))
</script>

<template>
  <OverviewPanel v-if="all.length" :title="title" :kicker="kicker" :full="full">
    <div class="chip-row">
      <span v-for="chip in shown" :key="chip" class="chip">{{ chip }}</span>
    </div>
    <button v-if="all.length > cap" type="button" class="btn--link chip-cloud__more" @click="showAll = !showAll">
      {{ showAll ? 'Show fewer' : `Show all ${all.length}` }}
    </button>
  </OverviewPanel>
</template>

<style scoped>
.chip-cloud__more { margin-top: 0.8rem; color: var(--gold); }
</style>
