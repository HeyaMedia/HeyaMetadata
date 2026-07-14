<script setup lang="ts">
import type { Fact } from '~/components/FactList.vue'
import type { EntityDocument } from '~/utils/types'

// release (a specific issue of an album).
const props = defineProps<{ entity: EntityDocument }>()
const data = computed<any>(() => props.entity.data ?? {})

const facts = computed<Fact[]>(() => {
  const d = data.value
  return [
    { label: 'Status', value: titleCase(d.status) },
    { label: 'Released', value: formatDate(d.date) },
    { label: 'Country', value: d.country },
    { label: 'Barcode', value: d.barcode },
    { label: 'Packaging', value: titleCase(d.packaging) },
    { label: 'Quality', value: titleCase(d.quality) },
    { label: 'Labels', value: (d.labels ?? []).map((label: any) => formatValue(label.name)) },
  ]
})

const media = computed<any[]>(() => (Array.isArray(data.value.media) ? data.value.media : []))
// Discs that carry a real tracklist — rendered as full tracklists so each track
// can link to its canonical recording (via recording_entity_id).
const trackDiscs = computed(() =>
  media.value
    .map((medium, index) => ({ medium, index }))
    .filter(({ medium }) => Array.isArray(medium.tracks) && medium.tracks.length),
)

function mediumTitle(medium: any, index: number): string {
  const label = media.value.length > 1 ? `Disc ${medium.position || index + 1}` : 'Tracklist'
  const format = formatValue(medium.format)
  return format && format.toLowerCase() !== 'cd' ? `${label} · ${format}` : label
}
</script>

<template>
  <div class="overview-grid">
    <OverviewPanel title="Overview" kicker="Combined record">
      <FactList :facts="facts" />
    </OverviewPanel>

    <ExternalIdsPanel :external-ids="entity.external_ids" />

    <TracklistPanel
      v-for="disc in trackDiscs"
      :key="disc.index"
      :tracks="disc.medium.tracks"
      :title="mediumTitle(disc.medium, disc.index)"
      :kicker="formatValue(disc.medium.format) || 'Recordings'"
    />

    <OverviewPanel v-if="!trackDiscs.length && media.length" title="Media" kicker="Physical structure" full>
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
