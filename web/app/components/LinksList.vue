<script setup lang="ts">
// External links. Normalizes the two shapes in the data: {kind,value} (movie)
// and {type,url} (artist/recording). Deduped by URL and capped with an expand
// so link-heavy artists don't dominate the page.
const props = defineProps<{ links?: any[] }>()

const CAP = 20

const allLinks = computed(() => {
  const seen = new Set<string>()
  const out: { label: string; url: string }[] = []
  for (const link of props.links ?? []) {
    const url = formatValue(link.url ?? link.value)
    if (!/^https?:\/\//i.test(url) || seen.has(url)) continue
    seen.add(url)
    out.push({ label: titleCase(link.type ?? link.kind ?? 'link'), url })
  }
  return out
})

const showAll = ref(false)
const links = computed(() => (showAll.value ? allLinks.value : allLinks.value.slice(0, CAP)))
</script>

<template>
  <OverviewPanel v-if="allLinks.length" title="Links" kicker="Off-platform">
    <div class="links">
      <a v-for="(link, index) in links" :key="index" :href="link.url" target="_blank" rel="noopener noreferrer" class="chip chip--accent">
        {{ link.label }} ↗
      </a>
    </div>
    <button v-if="allLinks.length > CAP" type="button" class="btn--link links__more" @click="showAll = !showAll">
      {{ showAll ? 'Show fewer' : `Show all ${allLinks.length}` }}
    </button>
  </OverviewPanel>
</template>

<style scoped>
.links { display: flex; flex-wrap: wrap; gap: 0.4rem; }
.links a { text-decoration: none; }
.links__more { margin-top: 0.8rem; color: var(--gold); }
</style>
