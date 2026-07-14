<script setup lang="ts">
// Standalone episode page (GET /episodes/{id}), rendered from Codex's episodic
// endpoint. Carries show + season back-links, a still, ratings, and multi-scheme
// numbering. Specials are read from is_special/episode_type — never inferred
// from a season-zero number.
const route = useRoute()
const api = useHeyaApi()
const id = computed(() => route.params.id as string)

const { data, pending, error } = await useAsyncData('episode', () => api.episode(id.value), { watch: [id] })

const ep = computed<any>(() => data.value?.data ?? {})
const show = computed(() => data.value?.show)
const title = computed(() => preferredText(ep.value.titles) || formatValue(ep.value.name) || 'Episode')

// Resolve the canonical season resource once for display numbering. Provider
// `aired` schemes can disagree (or flatten cours), while season_id is Heya's
// authoritative parent link.
const seasonId = computed(() => ep.value.season_id as string | undefined)
const { data: seasonResource } = await useAsyncData(
  `episode-season-${id.value}`,
  () => seasonId.value ? api.season(seasonId.value) : Promise.resolve(null),
  { watch: [seasonId] },
)
const canonicalSeason = computed<number | null>(() => {
  const number = Number(seasonResource.value?.data?.number)
  return Number.isFinite(number) ? number : null
})

const numbers = computed<any[]>(() => (Array.isArray(ep.value.numbers) ? ep.value.numbers : []))
const aired = computed(() => canonicalEpisodeNumber(ep.value, canonicalSeason.value) ?? {})
const absolute = computed(() => numbers.value.find(n => n?.scheme === 'absolute')?.number)

const synopsis = computed(() => {
  if (formatValue(ep.value.summary)) return formatValue(ep.value.summary)
  const items: any[] = Array.isArray(ep.value.overviews) ? ep.value.overviews : []
  return formatValue((items.find(o => o.language === 'en') ?? items[0])?.value)
})
const stillId = computed(() => {
  const images: any[] = Array.isArray(ep.value.images) ? ep.value.images : []
  return images.find(img => /still|screen/i.test(img.class || ''))?.id || images[0]?.id
})
const episodeType = computed(() => {
  const type = titleCase(ep.value.episode_type)
  return type && type.toLowerCase() !== 'regular' ? type : ''
})

// External_ids and image providers stand in for entity provenance, shown through
// the same Provenance panel used on entity pages.
const provenance = computed<Record<string, unknown[]>>(() => {
  const map: Record<string, unknown[]> = {}
  const ids: any[] = Array.isArray(ep.value.external_ids) ? ep.value.external_ids : []
  if (ids.length) map.identity = ids.map(e => ({ provider: e.provider, observation_id: [e.namespace, e.value].filter(Boolean).join(' · ') }))
  const images: any[] = Array.isArray(ep.value.images) ? ep.value.images : []
  if (images.length) map.artwork = images.map(i => ({ provider: i.provider, observation_id: [i.class, i.provider_id].filter(Boolean).join(' · ') }))
  return map
})

const facts = computed(() => [
  { label: 'Air date', value: formatDate(ep.value.air_date) },
  { label: 'Runtime', value: formatRuntime(ep.value.runtime_minutes) },
  { label: 'Season', value: aired.value.season != null ? `Season ${aired.value.season}` : '' },
  { label: 'Episode', value: aired.value.number },
  { label: 'Absolute №', value: absolute.value },
])

// --- SEO (reactive; the episode resource loads after mount) -----------------
const site = useSiteConfig()
const seoImage = computed(() => (stillId.value ? imageVariantUrl(site.url, stillId.value) : undefined))
const seoDescription = computed(() => {
  const label = show.value?.title ? `${title.value} from ${show.value.title}` : title.value
  return metaDescription(synopsis.value, `${label} — canonical episode metadata on Heya.`)
})

useSeoMeta({
  title: () => (data.value ? title.value : undefined),
  description: () => (data.value ? seoDescription.value : undefined),
  ogImage: () => seoImage.value,
  ogType: () => 'video.episode',
  twitterCard: () => (seoImage.value ? 'summary_large_image' : 'summary'),
})

useSchemaOrg(computed(() => {
  if (!data.value) return []
  const node: Record<string, any> = { '@type': 'TVEpisode', name: title.value, url: `${site.url}${route.path}` }
  if (synopsis.value) node.description = synopsis.value
  if (seoImage.value) node.image = seoImage.value
  if (aired.value.number != null) node.episodeNumber = aired.value.number
  return [node]
}))
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
          <template v-if="aired.season != null"><i aria-hidden="true" />S{{ aired.season }}·E{{ aired.number }}</template>
          <template v-if="absolute != null"><i aria-hidden="true" />#{{ absolute }} absolute</template>
        </p>
        <h1 class="editorial ep-hero__title">{{ title }}</h1>
        <div class="ep-hero__sub">
          <p v-if="show" class="ep-hero__show">from <NuxtLink :to="entityPath(show)">{{ show.title }}</NuxtLink></p>
          <span v-if="ep.is_special" class="badge badge--gold">Special</span>
          <span v-else-if="episodeType" class="badge">{{ episodeType }}</span>
        </div>
      </header>

      <DetailSections :images="(ep.images as any) || []" :provenance="provenance" :raw="data">
        <div v-if="stillId" class="ep-still">
          <MetadataImage :image-id="stillId" :alt="title" variant="hero" />
        </div>

        <div class="overview-grid ep-grid">
          <OverviewPanel title="Synopsis" kicker="Episode">
            <p v-if="synopsis" class="ep-summary">{{ synopsis }}</p>
            <p v-else class="muted">No synopsis available for this episode.</p>
          </OverviewPanel>
          <OverviewPanel title="Details" kicker="Broadcast">
            <FactList :facts="facts" />
          </OverviewPanel>
          <RatingsPanel :ratings="ep.ratings" />
          <OverviewPanel v-if="numbers.length" title="Numbering" kicker="Across providers" full>
            <div class="chip-row">
              <span v-for="(scheme, si) in numbers" :key="si" class="chip">
                {{ formatKey(scheme.scheme || 'num') }} · <template v-if="scheme.season != null">S{{ scheme.season }}·</template>E{{ scheme.number ?? '?' }}
              </span>
            </div>
          </OverviewPanel>
        </div>
      </DetailSections>
    </template>
  </div>
</template>

<style scoped>
.crumbs { display: flex; flex-wrap: wrap; gap: 1.25rem; margin-bottom: 1.25rem; font-size: 0.74rem; }
.crumbs a { color: var(--muted); }
.crumbs a:hover { color: var(--text); }
.ep-hero { padding: 0.5rem 0 1.25rem; }
.hero__kicker { display: flex; flex-wrap: wrap; align-items: center; gap: 0.6rem; margin: 0; color: #8b9697; font-family: var(--font-mono); font-size: 0.64rem; letter-spacing: 0.1em; text-transform: uppercase; }
.hero__kicker span { color: var(--gold); }
.hero__kicker i { width: 0.25rem; height: 0.25rem; border-radius: 50%; background: #536064; }
.ep-hero__title { margin: 0.6rem 0 0.5rem; font-size: clamp(1.9rem, 4vw, 3.2rem); }
.ep-hero__sub { display: flex; align-items: center; gap: 0.8rem; }
.ep-hero__show { margin: 0; color: var(--muted); font-size: 0.85rem; }
.ep-hero__show a { color: var(--gold); }
.badge { padding: 0.2rem 0.55rem; border: 1px solid var(--line-strong); border-radius: 2rem; color: var(--text-dim); font-size: 0.6rem; letter-spacing: 0.08em; text-transform: uppercase; }
.badge--gold { border-color: var(--gold); color: var(--gold); }
.ep-still { overflow: hidden; margin-bottom: 1.5rem; border: 1px solid var(--line); border-radius: var(--radius); aspect-ratio: 16 / 9; max-height: 26rem; }
.ep-summary { margin: 0; color: #a0aaa7; font-size: 0.86rem; line-height: 1.75; }
.muted { margin: 0; font-size: 0.8rem; }
</style>
