<script setup lang="ts">
// Standalone season page (GET /seasons/{id}) with show back-link and its
// episodes linking to individual episode pages.
const route = useRoute()
const api = useHeyaApi()
const id = computed(() => route.params.id as string)

const { data, pending, error } = await useAsyncData('season', () => api.season(id.value), { watch: [id] })

const season = computed(() => data.value?.data ?? {})
const show = computed(() => data.value?.show)
const name = computed(() => formatValue(season.value.name) || (season.value.number != null ? `Season ${season.value.number}` : 'Season'))
const episodes = computed(() => (Array.isArray(data.value?.episodes) ? data.value!.episodes! : []))

function epNumber(ep: any) {
  const numbers: any[] = Array.isArray(ep.numbers) ? ep.numbers : []
  return (numbers.find(n => n?.season != null) ?? numbers[0] ?? {}).number
}
function epTitle(ep: any) {
  return preferredText(ep.titles) || (epNumber(ep) != null ? `Episode ${epNumber(ep)}` : 'Episode')
}
</script>

<template>
  <div class="shell detail-page">
    <nav v-if="show" class="crumbs">
      <NuxtLink :to="entityPath(show)">← {{ show.title }}</NuxtLink>
    </nav>

    <EmptyState v-if="!data && !pending" title="Season unavailable." :message="error || 'This season could not be loaded.'" />

    <template v-else-if="data">
      <header class="season-hero">
        <p class="hero__kicker"><span>Season {{ season.number }}</span><template v-if="show"><i aria-hidden="true" />{{ show.title }}</template></p>
        <h1 class="editorial season-hero__title">{{ name }}</h1>
        <div class="chip-row season-hero__meta">
          <span v-if="formatDate(season.premiere_date)" class="chip">Premiered {{ formatDate(season.premiere_date) }}</span>
          <span v-if="season.episode_order" class="chip">{{ season.episode_order }} episodes ordered</span>
          <span v-if="episodes.length" class="chip">{{ episodes.length }} available</span>
        </div>
      </header>

      <section class="season-episodes">
        <header class="section-head"><div><span class="section-label">Structure</span><h2>Episodes</h2></div></header>
        <ul v-if="episodes.length" class="line-list">
          <li v-for="ep in episodes" :key="ep.id">
            <span class="line-list__index">{{ epNumber(ep) != null ? String(epNumber(ep)).padStart(2, '0') : '—' }}</span>
            <span class="line-list__main">
              <NuxtLink :to="`/episodes/${ep.id}`" class="season-episodes__link">{{ epTitle(ep) }} ↗</NuxtLink>
            </span>
            <span v-if="formatDate(ep.air_date)" class="line-list__meta">{{ formatDate(ep.air_date) }}</span>
          </li>
        </ul>
        <EmptyState v-else title="No episodes listed." message="This season carries no episode records in the canonical library." />
      </section>
    </template>
  </div>
</template>

<style scoped>
.crumbs { display: flex; gap: 1.25rem; margin-bottom: 1.25rem; font-size: 0.74rem; }
.crumbs a { color: var(--muted); }
.crumbs a:hover { color: var(--text); }
.season-hero { padding: 0.5rem 0 1.75rem; }
.hero__kicker { display: flex; align-items: center; gap: 0.6rem; margin: 0; color: #8b9697; font-family: var(--font-mono); font-size: 0.64rem; letter-spacing: 0.1em; text-transform: uppercase; }
.hero__kicker span { color: var(--gold); }
.hero__kicker i { width: 0.25rem; height: 0.25rem; border-radius: 50%; background: #536064; }
.season-hero__title { margin: 0.6rem 0 0.9rem; font-size: clamp(2rem, 4vw, 3.4rem); }
.section-head { margin-bottom: 1.1rem; padding-bottom: 0.75rem; border-bottom: 1px solid var(--line); }
.section-head h2 { margin: 0.3rem 0 0; font-size: 1.2rem; font-weight: 500; }
.season-episodes__link { color: var(--text-dim); }
.season-episodes__link:hover { color: var(--gold); }
</style>
