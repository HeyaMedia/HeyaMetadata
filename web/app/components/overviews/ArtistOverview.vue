<script setup lang="ts">
import type { Fact } from '~/components/FactList.vue'
import type { EntityDocument } from '~/utils/types'

const props = defineProps<{ entity: EntityDocument }>()
const data = computed<any>(() => props.entity.data ?? {})

const lifespan = computed(() => {
  const dates: any[] = data.value.lifecycle?.dates ?? []
  const begin = dates.find(d => d.type === 'begin')?.value
  const end = dates.find(d => d.type === 'end')?.value
  if (!begin && !end) return ''
  return [formatValue(begin) || '?', end ? formatValue(end) : (data.value.lifecycle?.ended ? '?' : 'present')].join(' – ')
})

const facts = computed<Fact[]>(() => {
  const d = data.value
  const metrics: Fact[] = (d.metrics ?? [])
    .filter((metric: any) => metric?.value != null)
    .map((metric: any) => ({ label: formatKey(metric.name), value: metric.value }))
  return [
    { label: 'Type', value: titleCase(d.classification?.artist_type) },
    { label: 'Area', value: d.areas },
    { label: 'Life span', value: lifespan.value },
    { label: 'Genres', value: d.genres },
    { label: 'Tags', value: (d.tags ?? []).map((tag: any) => tag.name) },
    ...metrics,
  ]
})

const names = computed(() => (data.value.names ?? []).filter((name: any) => formatValue(name.value)))
const similar = computed(() =>
  (data.value.similar_artists ?? [])
    .map((artist: any) => ({ name: formatValue(artist.name), url: formatValue(artist.url) }))
    .filter((artist: any) => artist.name),
)
</script>

<template>
  <div class="overview-grid">
    <OverviewPanel title="Overview" kicker="Combined record">
      <FactList :facts="facts" />
    </OverviewPanel>

    <ExternalIdsPanel :external-ids="entity.external_ids" />

    <OverviewPanel v-if="names.length" title="Names & aliases" kicker="Identity">
      <ul class="line-list">
        <li v-for="(name, index) in names" :key="index">
          <span class="line-list__main">
            <span class="line-list__title">{{ formatValue(name.value) }}</span>
          </span>
          <span v-if="name.primary || name.type" class="line-list__meta">{{ name.primary ? 'primary' : formatKey(name.type) }}</span>
        </li>
      </ul>
    </OverviewPanel>

    <OverviewPanel v-if="similar.length" title="Related artists" kicker="Neighbours">
      <div class="chip-row">
        <a v-for="(artist, index) in similar" :key="index" :href="artist.url || undefined" target="_blank" rel="noopener noreferrer" class="chip" :class="{ 'chip--accent': artist.url }">
          {{ artist.name }}<template v-if="artist.url"> ↗</template>
        </a>
      </div>
    </OverviewPanel>

    <LinksList :links="data.links" />
  </div>
</template>
