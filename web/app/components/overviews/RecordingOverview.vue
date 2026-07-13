<script setup lang="ts">
import type { Fact } from '~/components/FactList.vue'
import type { EntityDocument } from '~/utils/types'

// recording (a single track/performance).
const props = defineProps<{ entity: EntityDocument }>()
const data = computed<any>(() => props.entity.data ?? {})

const facts = computed<Fact[]>(() => {
  const d = data.value
  const rating = d.rating && d.rating.value != null
    ? `${d.rating.value}${d.rating.votes ? ` · ${formatCount(d.rating.votes)} votes` : ''}`
    : ''
  return [
    { label: 'Duration', value: formatDuration(d.duration_ms) },
    { label: 'ISRCs', value: d.isrcs },
    { label: 'Provider', value: formatKey(d.provider) },
    { label: 'Rating', value: rating },
    { label: 'Fingerprints', value: Array.isArray(d.fingerprints) ? d.fingerprints.length : '' },
  ]
})

const releases = computed(() => (Array.isArray(data.value.releases) ? data.value.releases : []))
</script>

<template>
  <div class="overview-grid">
    <OverviewPanel title="Overview" kicker="Combined record">
      <FactList :facts="facts" />
    </OverviewPanel>

    <ExternalIdsPanel :external-ids="entity.external_ids" />
    <LinksList :links="data.links" />

    <OverviewPanel v-if="releases.length" title="Appears on" kicker="Releases" full>
      <ol class="line-list">
        <li v-for="(release, index) in releases" :key="index">
          <span class="line-list__main">
            <span class="line-list__title">{{ formatValue(release.title) || 'Release' }}</span>
            <span class="line-list__sub">{{ [formatValue(release.country), titleCase(release.status)].filter(Boolean).join(' · ') }}</span>
          </span>
          <span class="line-list__meta">{{ formatDate(release.date) }}</span>
        </li>
      </ol>
    </OverviewPanel>

    <LyricsPanel :recording-id="entity.id" />
  </div>
</template>
