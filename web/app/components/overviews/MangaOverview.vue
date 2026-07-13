<script setup lang="ts">
import type { Fact } from '~/components/FactList.vue'
import type { EntityDocument } from '~/utils/types'

// manga (series).
const props = defineProps<{ entity: EntityDocument }>()
const data = computed<any>(() => props.entity.data ?? {})

const facts = computed<Fact[]>(() => {
  const d = data.value
  return [
    { label: 'Status', value: titleCase(d.status) },
    { label: 'Type', value: titleCase(d.subtype) },
    { label: 'Serialization', value: d.serialization },
    { label: 'Started', value: formatDate(d.start_date) },
    { label: 'Ended', value: formatDate(d.end_date) },
    { label: 'Volumes', value: d.volume_count },
    { label: 'Chapters', value: d.chapter_count },
  ]
})

const titles = computed(() => (data.value.titles ?? []).filter((title: any) => formatValue(title.value)))
</script>

<template>
  <div class="overview-grid">
    <OverviewPanel title="Overview" kicker="Combined record">
      <FactList :facts="facts" />
    </OverviewPanel>

    <ExternalIdsPanel :external-ids="entity.external_ids" />
    <RatingsPanel :ratings="data.ratings" />

    <OverviewPanel v-if="titles.length" title="Localized titles" kicker="Names">
      <ul class="line-list">
        <li v-for="(title, index) in titles" :key="index">
          <span class="line-list__main"><span class="line-list__title">{{ formatValue(title.value) }}</span></span>
          <span v-if="title.language" class="line-list__meta">{{ title.language }}</span>
        </li>
      </ul>
    </OverviewPanel>
  </div>
</template>
