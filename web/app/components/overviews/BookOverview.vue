<script setup lang="ts">
import type { Fact } from '~/components/FactList.vue'
import type { EntityDocument } from '~/utils/types'

// book_work.
const props = defineProps<{ entity: EntityDocument }>()
const data = computed<any>(() => props.entity.data ?? {})

const facts = computed<Fact[]>(() => {
  const d = data.value
  return [
    { label: 'Authors', value: (d.authors ?? []).map((author: any) => formatValue(author.name)) },
    { label: 'Editions', value: Array.isArray(d.editions) ? d.editions.length : '' },
    { label: 'Medium', value: titleCase(d.publication?.medium) },
    { label: 'Published', value: formatDate(d.publication?.first_published ?? d.first_publish_date) },
  ]
})

const subjects = computed(() => (Array.isArray(data.value.subjects) ? data.value.subjects : []))

// Series membership (data.series[]): "The Expanse #1". Deduped by name+position;
// linked to the canonical series entity only when entity_id is present.
const series = computed(() => {
  const seen = new Set<string>()
  const out: { name: string; position: string; to?: string }[] = []
  for (const item of data.value.series ?? []) {
    const name = formatValue(item?.name)
    if (!name) continue
    const position = formatValue(item?.position)
    const key = `${name.toLowerCase()}#${position}`
    if (seen.has(key)) continue
    seen.add(key)
    out.push({ name, position, to: item?.entity_id ? `/entities/${item.entity_id}` : undefined })
  }
  return out
})

const editions = computed(() => (Array.isArray(data.value.editions) ? data.value.editions : []))
const linkTag = resolveComponent('NuxtLink')
const EDITION_CAP = 20
const showAllEditions = ref(false)
const shownEditions = computed(() => (showAllEditions.value ? editions.value : editions.value.slice(0, EDITION_CAP)))
</script>

<template>
  <div class="overview-grid">
    <OverviewPanel title="Overview" kicker="Combined record">
      <FactList :facts="facts" />
    </OverviewPanel>

    <ExternalIdsPanel :external-ids="entity.external_ids" />

    <OverviewPanel v-if="series.length" title="Series" kicker="Part of">
      <ul class="series-list">
        <li v-for="(item, index) in series" :key="index">
          <component :is="item.to ? linkTag : 'span'" :to="item.to" class="series-row" :class="{ 'is-ghost': !item.to }">
            <strong>{{ item.name }}</strong>
            <span v-if="item.position" class="series-row__pos">#{{ item.position }}</span>
          </component>
        </li>
      </ul>
    </OverviewPanel>

    <ChipCloud title="Subjects" kicker="Themes" :items="subjects" full />

    <OverviewPanel v-if="editions.length" :title="`Editions (${editions.length})`" kicker="Publications" full>
      <ol class="line-list">
        <li v-for="(edition, index) in shownEditions" :key="edition.id || index">
          <span class="line-list__main">
            <span class="line-list__title">{{ formatValue(edition.title) || 'Edition' }}</span>
            <span class="line-list__sub">{{ [formatValue(edition.format), formatValue(edition.publishers), formatValue(edition.languages)].filter(Boolean).join(' · ') }}</span>
          </span>
        </li>
      </ol>
      <button v-if="editions.length > EDITION_CAP" type="button" class="btn--link editions__more" @click="showAllEditions = !showAllEditions">
        {{ showAllEditions ? 'Show fewer' : `Show all ${editions.length} editions` }}
      </button>
    </OverviewPanel>
  </div>
</template>

<style scoped>
.editions__more { margin-top: 0.85rem; color: var(--gold); }
.series-list { display: flex; flex-direction: column; gap: 0.4rem; margin: 0; padding: 0; list-style: none; }
.series-row { display: flex; align-items: baseline; gap: 0.5rem; font-size: 0.82rem; }
.series-row strong { font-weight: 500; color: var(--text-dim); }
a.series-row:hover strong { color: var(--gold); }
.series-row__pos { color: var(--gold); font-family: var(--font-mono); font-size: 0.72rem; }
.series-row.is-ghost strong { color: var(--muted); }
</style>
