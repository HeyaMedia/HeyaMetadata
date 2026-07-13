<script setup lang="ts">
import type { Fact } from '~/components/FactList.vue'
import type { EntityDocument } from '~/utils/types'

// Shared by tv_show and anime — both carry classification/lifecycle/episodes and
// now the enriched TV document (networks, companies, keywords, certifications,
// videos, recommendations).
const props = defineProps<{ entity: EntityDocument }>()

const data = computed<any>(() => props.entity.data ?? {})

const facts = computed<Fact[]>(() => {
  const d = data.value
  const c = d.classification ?? {}
  const life = d.lifecycle ?? {}
  const seasons = d.season_count ?? (Array.isArray(d.seasons) ? d.seasons.length : '')
  return [
    { label: 'Status', value: titleCase(c.status) },
    { label: 'Format', value: titleCase(c.format) },
    { label: 'First aired', value: formatDate(life.start_date) },
    { label: 'Last aired', value: formatDate(life.end_date) },
    { label: 'Episodes', value: d.episode_count },
    { label: 'Seasons', value: seasons },
    { label: 'Runtime', value: formatRuntime(d.runtime_minutes) },
    { label: 'Genres', value: c.genres },
    { label: 'Countries', value: c.countries },
    { label: 'Language', value: c.language },
  ]
})

// Content ratings arrive per-country ({country, rating, system}); collapse to a
// deduped "US TV-MA" style set rather than dozens of near-duplicates.
const certifications = computed<string[]>(() => {
  const seen = new Set<string>()
  const out: string[] = []
  for (const cert of data.value.certifications ?? []) {
    const label = [String(cert.country ?? '').toUpperCase(), formatValue(cert.rating)].filter(Boolean).join(' ')
    if (!label || seen.has(label)) continue
    seen.add(label)
    out.push(label)
  }
  return out
})
</script>

<template>
  <div>
    <div class="overview-grid">
      <OverviewPanel title="Overview" kicker="Combined record">
        <FactList :facts="facts" />
      </OverviewPanel>

      <ExternalIdsPanel :external-ids="entity.external_ids" />
      <AltTitles :titles="data.titles" :exclude="entity.presentation?.title || entity.display?.title" />
      <RatingsPanel :ratings="data.ratings" />
      <ChipCloud title="Keywords" kicker="Themes" :items="data.keywords" full />
      <ChipCloud title="Certifications" kicker="Content ratings" :items="certifications" />
      <LinksList :links="data.links" />
      <StudiosStrip :studios="data.networks" title="Networks" kicker="Broadcast" />
      <StudiosStrip :studios="data.organizations" title="Companies" kicker="Production" />
    </div>

    <SeasonBrowser :seasons="data.seasons" :episodes="data.episodes" />
    <VideoGallery :videos="data.videos" />
    <CastSection :entity-id="entity.id" />
    <RecommendationsRail :recommendations="data.recommendations" :kind="entity.kind" />
  </div>
</template>
