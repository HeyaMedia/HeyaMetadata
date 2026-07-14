<script setup lang="ts">
// Standalone season page (GET /seasons/{id}) with show back-link, poster art,
// overview, and its episodes linking to individual episode pages.
const route = useRoute()
const api = useHeyaApi()
const id = computed(() => route.params.id as string)

const { data, pending, error } = await useAsyncData('season', () => api.season(id.value), { watch: [id] })

const season = computed<any>(() => data.value?.data ?? {})
const show = computed(() => data.value?.show)
const name = computed(() => formatValue(season.value.name) || (season.value.number != null ? `Season ${season.value.number}` : 'Season'))
const episodes = computed<any[]>(() => (Array.isArray(data.value?.episodes) ? data.value!.episodes! : []))

// Prefer a season poster; fall back to any season image, then the show poster.
const posterId = computed(() => {
  const images: any[] = Array.isArray(season.value.images) ? season.value.images : []
  return images.find(img => img.class === 'poster')?.id || images[0]?.id || show.value?.image_id
})
const overview = computed(() => {
  const items: any[] = Array.isArray(season.value.overviews) ? season.value.overviews : []
  return formatValue((items.find(o => o.language === 'en') ?? items[0])?.value)
})

// Seasons carry no entity-style provenance map, but external_ids and image
// providers are genuine "where it came from" evidence — surfaced through the
// same Provenance panel used on entity pages.
const provenance = computed<Record<string, unknown[]>>(() => {
  const map: Record<string, unknown[]> = {}
  const ids: any[] = Array.isArray(season.value.external_ids) ? season.value.external_ids : []
  if (ids.length) map.identity = ids.map(e => ({ provider: e.provider, observation_id: [e.namespace, e.value].filter(Boolean).join(' · ') }))
  const images: any[] = Array.isArray(season.value.images) ? season.value.images : []
  if (images.length) map.artwork = images.map(i => ({ provider: i.provider, observation_id: [i.class, i.provider_id].filter(Boolean).join(' · ') }))
  return map
})

function epNumber(ep: any) {
  const numbers: any[] = Array.isArray(ep.numbers) ? ep.numbers : []
  return (numbers.find(n => n?.scheme === 'aired') ?? numbers.find(n => n?.season != null) ?? numbers[0] ?? {}).number
}
function epTitle(ep: any) {
  return preferredText(ep.titles) || (epNumber(ep) != null ? `Episode ${epNumber(ep)}` : 'Episode')
}

// --- SEO (reactive; the season resource loads after mount) ------------------
const site = useSiteConfig()
const seoImage = computed(() => (posterId.value ? imageVariantUrl(site.url, posterId.value) : undefined))
const seoDescription = computed(() => {
  const label = show.value?.title ? `${name.value} of ${show.value.title}` : name.value
  return metaDescription(overview.value, `${label} — canonical episode list and metadata on Heya.`)
})

useSeoMeta({
  title: () => (data.value ? name.value : undefined),
  description: () => (data.value ? seoDescription.value : undefined),
  ogImage: () => seoImage.value,
  ogType: () => 'video.tv_show',
  twitterCard: () => (seoImage.value ? 'summary_large_image' : 'summary'),
})

useSchemaOrg(computed(() => {
  if (!data.value) return []
  const node: Record<string, any> = { '@type': 'TVSeason', name: name.value, url: `${site.url}${route.path}` }
  if (overview.value) node.description = overview.value
  if (seoImage.value) node.image = seoImage.value
  if (season.value.number != null) node.seasonNumber = season.value.number
  return [node]
}))
</script>

<template>
  <div class="shell detail-page">
    <nav v-if="show" class="crumbs">
      <NuxtLink :to="entityPath(show)">← {{ show.title }}</NuxtLink>
    </nav>

    <EmptyState v-if="!data && !pending" title="Season unavailable." :message="error || 'This season could not be loaded.'" />

    <template v-else-if="data">
      <header class="season-hero">
        <div class="season-hero__art">
          <MetadataImage :image-id="posterId" :alt="name" variant="card" />
        </div>
        <div class="season-hero__body">
          <p class="hero__kicker"><span>Season {{ season.number }}</span><template v-if="show"><i aria-hidden="true" />{{ show.title }}</template></p>
          <h1 class="editorial season-hero__title">{{ name }}</h1>
          <div class="chip-row season-hero__meta">
            <span v-if="season.status" class="chip">{{ titleCase(season.status) }}</span>
            <span v-if="formatDate(season.premiere_date)" class="chip">Premiered {{ formatDate(season.premiere_date) }}</span>
            <span v-if="formatDate(season.end_date)" class="chip">Ended {{ formatDate(season.end_date) }}</span>
            <span v-if="season.episode_count" class="chip">{{ season.episode_count }} episodes</span>
            <span v-else-if="episodes.length" class="chip">{{ episodes.length }} available</span>
          </div>
          <p v-if="overview" class="season-hero__overview">{{ overview }}</p>
        </div>
      </header>

      <DetailSections :images="(season.images as any) || []" :provenance="provenance" :raw="data">
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
      </DetailSections>
    </template>
  </div>
</template>

<style scoped>
.crumbs { display: flex; gap: 1.25rem; margin-bottom: 1.25rem; font-size: 0.74rem; }
.crumbs a { color: var(--muted); }
.crumbs a:hover { color: var(--text); }
.season-hero {
  display: grid;
  grid-template-columns: minmax(9rem, 13rem) 1fr;
  gap: clamp(1.5rem, 4vw, 3rem);
  align-items: start;
  padding: 0.5rem 0 1.75rem;
}
.season-hero__art { overflow: hidden; border: 1px solid var(--line); border-radius: var(--radius); aspect-ratio: 2 / 3; }
.hero__kicker { display: flex; align-items: center; gap: 0.6rem; margin: 0; color: #8b9697; font-family: var(--font-mono); font-size: 0.64rem; letter-spacing: 0.1em; text-transform: uppercase; }
.hero__kicker span { color: var(--gold); }
.hero__kicker i { width: 0.25rem; height: 0.25rem; border-radius: 50%; background: #536064; }
.season-hero__title { margin: 0.6rem 0 0.9rem; font-size: clamp(2rem, 4vw, 3.4rem); }
.season-hero__overview { max-width: 46rem; margin: 1rem 0 0; color: #97a19f; font-size: 0.85rem; line-height: 1.75; }
.section-head { margin-bottom: 1.1rem; padding-bottom: 0.75rem; border-bottom: 1px solid var(--line); }
.section-head h2 { margin: 0.3rem 0 0; font-size: 1.2rem; font-weight: 500; }
.season-episodes__link { color: var(--text-dim); }
.season-episodes__link:hover { color: var(--gold); }

@media (max-width: 620px) {
  .season-hero { grid-template-columns: 1fr; }
  .season-hero__art { max-width: 12rem; }
}
</style>
