<script setup lang="ts">
import { kindLabel } from '~/utils/kinds'

// Coverage/debugging page — real totals, not social proof. Charts are CSS-native.
const api = useHeyaApi()
const { data: stats, pending } = await useAsyncData('stats', () => api.stats(), { default: () => ({}) })

const kindRows = computed(() =>
  Object.entries(stats.value?.kinds ?? {})
    .map(([kind, count]) => ({ kind, label: kindLabel(kind), count: Number(count) }))
    .sort((a, b) => b.count - a.count),
)
const providerRows = computed(() =>
  Object.entries(stats.value?.provider_claims ?? {})
    .map(([provider, count]) => ({ provider, count: Number(count) }))
    .sort((a, b) => b.count - a.count),
)
const maxKind = computed(() => Math.max(1, ...kindRows.value.map(r => r.count)))
const maxProvider = computed(() => Math.max(1, ...providerRows.value.map(r => r.count)))

const imageCoverage = computed(() => {
  const total = Number(stats.value?.images ?? 0)
  const ready = Number(stats.value?.materialized_images ?? 0)
  return { total, ready, pct: total ? Math.round((ready / total) * 100) : 0 }
})
const freshness = computed(() => {
  const fresh = Number(stats.value?.fresh ?? 0)
  const stale = Number(stats.value?.stale ?? 0)
  const total = fresh + stale
  return { fresh, stale, pct: total ? Math.round((fresh / total) * 100) : 0 }
})

const generatedAt = computed(() => (stats.value?.generated_at ? new Date(stats.value.generated_at).toLocaleString() : ''))
</script>

<template>
  <div class="shell page">
    <header class="page-heading">
      <div>
        <span class="section-label">Coverage, not vanity</span>
        <h1>Library stats</h1>
        <p v-if="generatedAt">Generated {{ generatedAt }}</p>
      </div>
    </header>

    <LoadingSkeleton v-if="pending" layout="grid" shape="landscape" :count="4" />

    <template v-else>
      <div class="stat-hero">
        <div><strong>{{ formatCount(stats.entities) }}</strong><span>canonical entities</span></div>
        <div><strong>{{ formatCount(stats.provider_records) }}</strong><span>normalized provider records</span></div>
        <div><strong>{{ formatCount(stats.images) }}</strong><span>image candidates</span></div>
        <div><strong>{{ formatCount(stats.materialized_images) }}</strong><span>materialized images</span></div>
      </div>

      <div class="stat-meters">
        <article>
          <span class="section-label">Image coverage</span>
          <p class="stat-meter__value">{{ imageCoverage.pct }}%</p>
          <div class="meter"><i :style="{ width: `${imageCoverage.pct}%` }" /></div>
          <small>{{ formatCount(imageCoverage.ready) }} of {{ formatCount(imageCoverage.total) }} materialized</small>
        </article>
        <article>
          <span class="section-label">Projection freshness</span>
          <p class="stat-meter__value">{{ freshness.pct }}%</p>
          <div class="meter"><i class="is-green" :style="{ width: `${freshness.pct}%` }" /></div>
          <small>{{ formatCount(freshness.fresh) }} fresh · {{ formatCount(freshness.stale) }} stale</small>
        </article>
      </div>

      <div class="stat-columns">
        <article class="panel">
          <header><span class="section-label">Entity coverage</span><h2>By canonical kind</h2></header>
          <div v-for="row in kindRows" :key="row.kind" class="bar-row">
            <span class="bar-row__label">{{ row.label }}</span>
            <span class="bar-row__track"><b :style="{ width: `${Math.max(3, (row.count / maxKind) * 100)}%` }" /></span>
            <span class="bar-row__value">{{ formatCount(row.count) }}</span>
          </div>
        </article>

        <article class="panel">
          <header><span class="section-label">Identity coverage</span><h2>Accepted provider claims</h2></header>
          <div v-for="row in providerRows" :key="row.provider" class="bar-row">
            <span class="bar-row__label">{{ formatKey(row.provider) }}</span>
            <span class="bar-row__track"><b :style="{ width: `${Math.max(3, (row.count / maxProvider) * 100)}%` }" /></span>
            <span class="bar-row__value">{{ formatCount(row.count) }}</span>
          </div>
        </article>
      </div>
    </template>
  </div>
</template>

<style scoped>
.stat-hero {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  overflow: hidden;
  margin-bottom: 1rem;
  border: 1px solid var(--line);
  border-radius: var(--radius);
}
.stat-hero div { padding: 1.4rem 1.5rem; border-right: 1px solid var(--line); background: var(--panel); }
.stat-hero div:last-child { border-right: 0; }
.stat-hero strong { display: block; font-family: var(--font-mono); font-size: 1.9rem; font-weight: 400; }
.stat-hero span { display: block; margin-top: 0.35rem; color: var(--muted-2); font-size: 0.62rem; }

.stat-meters { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; margin-bottom: 1rem; }
.stat-meters article { padding: 1.4rem; border: 1px solid var(--line); border-radius: var(--radius); background: var(--panel); }
.stat-meter__value { margin: 0.6rem 0 0.7rem; font-family: var(--font-mono); font-size: 2rem; }
.meter { height: 0.4rem; overflow: hidden; border-radius: 1rem; background: #252d31; }
.meter i { display: block; height: 100%; border-radius: inherit; background: var(--gold); }
.meter i.is-green { background: var(--green); }
.stat-meters small { display: block; margin-top: 0.6rem; color: var(--muted-2); font-size: 0.66rem; }

.stat-columns { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; }
.stat-columns h2 { margin: 0.4rem 0 0; font-size: 1.1rem; font-weight: 500; }
.stat-columns header { margin-bottom: 1.25rem; }
.bar-row {
  display: grid;
  grid-template-columns: 8rem 1fr 3rem;
  align-items: center;
  gap: 1rem;
  padding: 0.5rem 0;
  border-top: 1px solid var(--line-soft);
  font-size: 0.68rem;
}
.bar-row:first-of-type { border-top: 0; }
.bar-row__label { color: var(--text-dim); text-transform: capitalize; }
.bar-row__track { overflow: hidden; height: 0.35rem; border-radius: 1rem; background: #252d31; }
.bar-row__track b { display: block; height: 100%; border-radius: inherit; background: var(--gold); }
.bar-row__value { color: var(--muted); font-family: var(--font-mono); text-align: right; }

@media (max-width: 860px) {
  .stat-hero { grid-template-columns: repeat(2, 1fr); }
  .stat-hero div:nth-child(2) { border-right: 0; }
  .stat-meters, .stat-columns { grid-template-columns: 1fr; }
}
</style>
