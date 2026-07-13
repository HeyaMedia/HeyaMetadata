<script setup lang="ts">
// "Also known as" — distinct localized titles from data.titles, excluding the
// one already shown as the main title. Uses data we already have but never
// surface elsewhere.
const props = withDefaults(defineProps<{
  titles?: any[]
  exclude?: string
  limit?: number
}>(), { titles: () => [], exclude: '', limit: 14 })

interface Alt { value: string; language: string }

const alts = computed<Alt[]>(() => {
  const seen = new Set<string>()
  const excluded = props.exclude.trim().toLowerCase()
  if (excluded) seen.add(excluded)
  const out: Alt[] = []
  for (const title of props.titles ?? []) {
    const value = formatValue(title?.value ?? title)
    const key = value.toLowerCase()
    if (!value || seen.has(key)) continue
    seen.add(key)
    out.push({ value, language: formatValue(title?.language) })
    if (out.length >= props.limit) break
  }
  return out
})
</script>

<template>
  <OverviewPanel v-if="alts.length" title="Also known as" kicker="Localized titles">
    <ul class="alt-titles">
      <li v-for="(alt, index) in alts" :key="index">
        <span class="alt-titles__value">{{ alt.value }}</span>
        <span v-if="alt.language" class="alt-titles__lang">{{ alt.language }}</span>
      </li>
    </ul>
  </OverviewPanel>
</template>

<style scoped>
.alt-titles { display: flex; flex-direction: column; margin: 0; padding: 0; list-style: none; }
.alt-titles li {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  align-items: baseline;
  padding: 0.5rem 0;
  border-top: 1px solid var(--line-soft);
  font-size: 0.76rem;
}
.alt-titles li:first-child { border-top: 0; }
.alt-titles__value { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.alt-titles__lang { flex: 0 0 auto; color: var(--muted-2); font-family: var(--font-mono); font-size: 0.62rem; text-transform: uppercase; }
</style>
