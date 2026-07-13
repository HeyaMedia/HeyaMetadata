<script setup lang="ts">
// Standalone episode page (GET /episodes/{id}), rendered from Codex's episodic
// endpoint. Carries show + season back-links for the entity graph.
const route = useRoute()
const api = useHeyaApi()
const id = computed(() => route.params.id as string)

const { data, pending, error } = await useAsyncData('episode', () => api.episode(id.value), { watch: [id] })

const ep = computed(() => data.value?.data ?? {})
const show = computed(() => data.value?.show)
const title = computed(() => preferredText(ep.value.titles) || formatValue(ep.value.name) || 'Episode')
const primary = computed(() => {
  const numbers: any[] = Array.isArray(ep.value.numbers) ? ep.value.numbers : []
  return numbers.find(n => n?.season != null) ?? numbers[0] ?? {}
})
const facts = computed(() => [
  { label: 'Air date', value: formatDate(ep.value.air_date) },
  { label: 'Runtime', value: formatRuntime(ep.value.runtime_minutes) },
  { label: 'Season', value: primary.value.season != null ? `Season ${primary.value.season}` : '' },
  { label: 'Episode', value: primary.value.number },
])
</script>

<template>
  <div class="shell detail-page">
    <nav v-if="show" class="crumbs">
      <NuxtLink :to="entityPath(show)">← {{ show.title }}</NuxtLink>
      <NuxtLink v-if="ep.season_id" :to="`/seasons/${ep.season_id}`">Season</NuxtLink>
    </nav>

    <EmptyState v-if="!data && !pending" title="Episode unavailable." :message="error || 'This episode could not be loaded.'" />

    <template v-else-if="data">
      <header class="ep-hero">
        <p class="hero__kicker">
          <span>Episode</span>
          <template v-if="primary.season != null"><i aria-hidden="true" />S{{ primary.season }}·E{{ primary.number }}</template>
        </p>
        <h1 class="editorial ep-hero__title">{{ title }}</h1>
        <p v-if="show" class="ep-hero__show">from <NuxtLink :to="entityPath(show)">{{ show.title }}</NuxtLink></p>
      </header>

      <div class="overview-grid ep-grid">
        <OverviewPanel title="Synopsis" kicker="Episode">
          <p v-if="formatValue(ep.summary)" class="ep-summary">{{ ep.summary }}</p>
          <p v-else class="muted">No synopsis available for this episode.</p>
        </OverviewPanel>
        <OverviewPanel title="Details" kicker="Broadcast">
          <FactList :facts="facts" />
        </OverviewPanel>
        <OverviewPanel v-if="Array.isArray(ep.numbers) && ep.numbers.length" title="Numbering" kicker="Across providers" full>
          <div class="chip-row">
            <span v-for="(scheme, si) in ep.numbers" :key="si" class="chip">
              {{ formatKey(scheme.scheme || 'num') }} · S{{ scheme.season ?? '?' }}·E{{ scheme.number ?? '?' }}
            </span>
          </div>
        </OverviewPanel>
      </div>
    </template>
  </div>
</template>

<style scoped>
.crumbs { display: flex; flex-wrap: wrap; gap: 1.25rem; margin-bottom: 1.25rem; font-size: 0.74rem; }
.crumbs a { color: var(--muted); }
.crumbs a:hover { color: var(--text); }
.ep-hero { padding: 0.5rem 0 1.5rem; }
.hero__kicker { display: flex; align-items: center; gap: 0.6rem; margin: 0; color: #8b9697; font-family: var(--font-mono); font-size: 0.64rem; letter-spacing: 0.1em; text-transform: uppercase; }
.hero__kicker span { color: var(--gold); }
.hero__kicker i { width: 0.25rem; height: 0.25rem; border-radius: 50%; background: #536064; }
.ep-hero__title { margin: 0.6rem 0 0.4rem; font-size: clamp(1.9rem, 4vw, 3.2rem); }
.ep-hero__show { margin: 0; color: var(--muted); font-size: 0.85rem; }
.ep-hero__show a { color: var(--gold); }
.ep-summary { margin: 0; color: #a0aaa7; font-size: 0.86rem; line-height: 1.75; }
.muted { margin: 0; font-size: 0.8rem; }
</style>
