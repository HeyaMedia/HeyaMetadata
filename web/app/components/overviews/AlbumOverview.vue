<script setup lang="ts">
import type { Fact } from '~/components/FactList.vue'
import type { EntityDocument } from '~/utils/types'

// release_group / album.
const props = defineProps<{ entity: EntityDocument }>()
const data = computed<any>(() => props.entity.data ?? {})

const facts = computed<Fact[]>(() => {
  const d = data.value
  const firstRelease = (d.dates ?? []).find((date: any) => date.type === 'first_release')?.value
  return [
    { label: 'Artist', value: firstValue(props.entity.display?.artist_credit, artistCreditLine(d.artist_credits)) },
    { label: 'Type', value: titleCase(d.classification?.primary_type) },
    { label: 'Secondary types', value: (d.classification?.secondary_types ?? []).map((t: any) => titleCase(t)) },
    { label: 'Released', value: formatDate(firstRelease) },
    { label: 'Genres', value: d.genres },
    { label: 'Tags', value: (d.tags ?? []).map((tag: any) => tag.name) },
  ]
})

const tracks = computed(() => (Array.isArray(data.value.tracks) ? data.value.tracks : []))
</script>

<template>
  <div class="overview-grid">
    <OverviewPanel title="Overview" kicker="Combined record">
      <FactList :facts="facts" />
    </OverviewPanel>

    <ExternalIdsPanel :external-ids="entity.external_ids" />
    <LinksList :links="data.links" />
    <RatingsPanel :ratings="data.ratings" />

    <TracklistPanel :tracks="tracks" />

    <RelationsList :entity-id="entity.id" type="editions" title="Releases" kicker="Editions" />
  </div>
</template>
