<script setup lang="ts">
import type { Fact } from '~/components/FactList.vue'
import type { EntityDocument } from '~/utils/types'

// release (a specific issue of an album).
const props = defineProps<{ entity: EntityDocument }>()
const data = computed<any>(() => props.entity.data ?? {})

const facts = computed<Fact[]>(() => {
  const d = data.value
  return [
    { label: 'Artist', value: firstValue(props.entity.display?.artist_credit, artistCreditLine(d.artist_credits)) },
    { label: 'Status', value: titleCase(d.status) },
    { label: 'Released', value: formatDate(d.date) },
    { label: 'Country', value: d.country },
    { label: 'Barcode', value: d.barcode },
    { label: 'Packaging', value: titleCase(d.packaging) },
    { label: 'Quality', value: titleCase(d.quality) },
    { label: 'Labels', value: (d.labels ?? []).map((label: any) => formatValue(label.name)) },
  ]
})

const media = computed(() => (Array.isArray(data.value.media) ? data.value.media : []))
</script>

<template>
  <div class="overview-grid">
    <OverviewPanel title="Overview" kicker="Combined record">
      <FactList :facts="facts" />
    </OverviewPanel>

    <ExternalIdsPanel :external-ids="entity.external_ids" />

    <OverviewPanel v-if="media.length" title="Media" kicker="Physical structure" full>
      <ol class="line-list">
        <li v-for="(medium, index) in media" :key="index">
          <span class="line-list__index">{{ medium.position || index + 1 }}</span>
          <span class="line-list__main">
            <span class="line-list__title">{{ formatValue(medium.format) || 'Medium' }}</span>
          </span>
          <span v-if="medium.track_count" class="line-list__meta">{{ medium.track_count }} tracks</span>
        </li>
      </ol>
    </OverviewPanel>
  </div>
</template>
