<script setup lang="ts">
import type { Fact } from '~/components/FactList.vue'
import type { EntityDocument } from '~/utils/types'

// Shared by tv_show and anime — both carry classification/lifecycle/episodes.
const props = defineProps<{ entity: EntityDocument }>()

const data = computed<any>(() => props.entity.data ?? {})

const facts = computed<Fact[]>(() => {
  const d = data.value
  const c = d.classification ?? {}
  const life = d.lifecycle ?? {}
  return [
    { label: 'Status', value: titleCase(c.status) },
    { label: 'Format', value: titleCase(c.format) },
    { label: 'First aired', value: formatDate(life.start_date) },
    { label: 'Last aired', value: formatDate(life.end_date) },
    { label: 'Episodes', value: d.episode_count },
    { label: 'Seasons', value: Array.isArray(d.seasons) ? d.seasons.length : '' },
    { label: 'Runtime', value: formatRuntime(d.runtime_minutes) },
    { label: 'Networks', value: d.networks },
    { label: 'Studios', value: d.studios },
    { label: 'Genres', value: c.genres },
    { label: 'Countries', value: c.countries },
    { label: 'Language', value: c.language },
  ]
})

const seasons = computed(() => (Array.isArray(data.value.seasons) ? data.value.seasons : []))
</script>

<template>
  <div>
    <div class="overview-grid">
      <OverviewPanel title="Overview" kicker="Combined record">
        <FactList :facts="facts" />
      </OverviewPanel>

      <ExternalIdsPanel :external-ids="entity.external_ids" />
      <RatingsPanel :ratings="data.ratings" />

      <OverviewPanel v-if="seasons.length" title="Seasons" kicker="Structure" full>
        <ol class="line-list">
          <li v-for="season in seasons" :key="season.number">
            <span class="line-list__index">{{ String(season.number).padStart(2, '0') }}</span>
            <span class="line-list__main">
              <span class="line-list__title">{{ formatValue(season.name) || `Season ${season.number}` }}</span>
              <span v-if="season.episode_order" class="line-list__sub">{{ season.episode_order }} episodes</span>
            </span>
            <span v-if="formatDate(season.premiere_date)" class="line-list__meta">{{ formatDate(season.premiere_date) }}</span>
          </li>
        </ol>
      </OverviewPanel>
    </div>

    <CreditsRail :credits="data.credits" title="Cast" kicker="People" />
  </div>
</template>
