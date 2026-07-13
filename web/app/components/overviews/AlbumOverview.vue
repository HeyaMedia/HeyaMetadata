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
const editions = computed(() => (Array.isArray(data.value.editions) ? data.value.editions : []))
</script>

<template>
  <div class="overview-grid">
    <OverviewPanel title="Overview" kicker="Combined record">
      <FactList :facts="facts" />
    </OverviewPanel>

    <ExternalIdsPanel :external-ids="entity.external_ids" />
    <LinksList :links="data.links" />
    <RatingsPanel :ratings="data.ratings" />

    <OverviewPanel v-if="tracks.length" title="Tracklist" kicker="Recordings" full>
      <ol class="line-list">
        <li v-for="(track, index) in tracks" :key="index">
          <span class="line-list__index">{{ track.position || track.number || index + 1 }}</span>
          <span class="line-list__main">
            <span class="line-list__title">{{ formatValue(track.title) || 'Untitled' }}</span>
            <span v-if="track.artist_credits" class="line-list__sub">{{ artistCreditLine(track.artist_credits) }}</span>
          </span>
          <span v-if="track.provider" class="line-list__meta">{{ track.provider }}</span>
        </li>
      </ol>
    </OverviewPanel>

    <OverviewPanel v-if="editions.length" title="Releases" kicker="Editions" full>
      <ol class="line-list">
        <li v-for="(edition, index) in editions" :key="index">
          <span class="line-list__main">
            <span class="line-list__title">{{ formatValue(edition.title) || 'Release' }}</span>
            <span class="line-list__sub">{{ [formatValue(edition.country), titleCase(edition.status)].filter(Boolean).join(' · ') }}</span>
          </span>
          <span class="line-list__meta">{{ formatDate(edition.date) }}</span>
        </li>
      </ol>
    </OverviewPanel>
  </div>
</template>
