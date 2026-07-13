<script setup lang="ts">
import type { Fact } from '~/components/FactList.vue'
import type { EntityDocument } from '~/utils/types'

const props = defineProps<{ entity: EntityDocument }>()

const data = computed<any>(() => props.entity.data ?? {})

function money(value: any): string {
  if (!value || value.amount == null) return ''
  const amount = Number(value.amount)
  if (!Number.isFinite(amount) || amount === 0) return ''
  try {
    return amount.toLocaleString('en-US', { style: 'currency', currency: value.currency || 'USD', maximumFractionDigits: 0 })
  } catch {
    return formatCount(amount)
  }
}

const facts = computed<Fact[]>(() => {
  const d = data.value
  const c = d.classification ?? {}
  const m = d.measurements ?? {}
  return [
    { label: 'Status', value: titleCase(d.release?.normalized_status ?? d.release?.raw_status) },
    { label: 'Runtime', value: formatRuntime(m.runtime_minutes) },
    { label: 'Original language', value: c.original_language },
    { label: 'Countries', value: c.countries },
    { label: 'Genres', value: c.genres },
    { label: 'Budget', value: money(m.budget) },
    { label: 'Revenue', value: money(m.revenue) },
    { label: 'Popularity', value: typeof m.popularity === 'number' ? Math.round(m.popularity) : '' },
  ]
})

const collection = computed(() => {
  const c = data.value.collection
  return c && c.provider_id ? c : null
})
</script>

<template>
  <div>
    <div class="overview-grid">
      <OverviewPanel title="Overview" kicker="Combined record">
        <FactList :facts="facts" />
      </OverviewPanel>

      <ExternalIdsPanel :external-ids="entity.external_ids" />

      <OverviewPanel v-if="collection" title="Collection" kicker="Franchise">
        <NuxtLink :to="`/collections/${collection.provider_id}`" class="collection-link">
          <strong>{{ collection.name }}</strong>
          <span>{{ collection.members?.length || 0 }} films ↗</span>
        </NuxtLink>
        <p v-if="collection.overview" class="collection-overview">{{ collection.overview }}</p>
      </OverviewPanel>

      <LinksList :links="data.links" />
      <RatingsPanel :ratings="data.ratings" />
    </div>

    <CreditsRail :credits="data.credits" title="Cast & crew" kicker="People" />
  </div>
</template>

<style scoped>
.collection-link { display: flex; flex-direction: column; gap: 0.2rem; }
.collection-link strong { font-size: 0.9rem; }
.collection-link span { color: var(--gold); font-size: 0.72rem; }
.collection-link:hover span { text-decoration: underline; }
.collection-overview { margin: 0.8rem 0 0; color: var(--muted); font-size: 0.76rem; line-height: 1.65; }
</style>
