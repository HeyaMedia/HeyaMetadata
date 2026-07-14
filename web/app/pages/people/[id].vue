<script setup lang="ts">
import type { Fact } from '~/components/FactList.vue'

// Canonical person page (GET /api/v2/persons/{id}) with biography, personal
// facts, external identity links, and the combined filmography. Person reads are
// demand-enriched in the background, so a cold TMDB person is polled a few times
// until it turns fresh (never forever — TVMaze-only people have no TMDB path).
const route = useRoute()
const api = useHeyaApi()
const id = computed(() => route.params.id as string)

const { data, pending, error, refresh } = await useAsyncData('person-doc', () => api.person(id.value), { watch: [id] })

// Canonical filmography comes from the paginated /persons/{id}/credits endpoint
// rather than the person document's embedded preview.
const { data: filmography } = await useAsyncData(
  'person-credits',
  () => api.allPersonCredits(id.value).catch(() => null),
  { watch: [id] },
)

let polls = 0
watch(id, () => { polls = 0 })
watch(data, doc => {
  if (!doc) return
  const hasTmdb = (doc.external_ids ?? []).some(ext => ext.provider === 'tmdb')
  if (doc.freshness?.state === 'stale' && hasTmdb && polls < 4) {
    polls++
    setTimeout(() => refresh(), 1600)
  } else if (doc.freshness?.state && doc.freshness.state !== 'stale') {
    polls = 0
  }
}, { immediate: true })

const info = computed<any>(() => data.value?.data ?? {})
const name = computed(() => formatValue(data.value?.display?.title) || 'Person')
const image = computed(() => data.value?.display?.image_id)
const credits = computed(() => filmography.value?.credits ?? info.value.credits ?? [])
const creditTotal = computed(() => filmography.value?.total ?? info.value.credit_total ?? credits.value.length)
const aliases = computed(() => (info.value.names ?? []).filter((n: string) => formatValue(n) && formatValue(n) !== name.value))
const knownFor = computed(() => formatValue(info.value.known_for_department))
const enriching = computed(() => data.value?.freshness?.state === 'stale')

const facts = computed<Fact[]>(() => {
  const born = [formatDate(info.value.birth_date), formatValue(info.value.place_of_birth)].filter(Boolean).join(' · ')
  return [
    { label: 'Born', value: born },
    { label: 'Died', value: formatDate(info.value.death_date) },
    { label: 'Gender', value: titleCase(info.value.gender) },
    { label: 'Known for', value: knownFor.value },
    { label: 'Popularity', value: typeof info.value.popularity === 'number' ? Math.round(info.value.popularity) : '' },
  ]
})

const biography = computed(() => formatValue(info.value.biography))
const bioLong = computed(() => biography.value.length > 420)
const bioExpanded = ref(false)

const PROVIDER_URL: Record<string, (v: string) => string> = {
  imdb: v => `https://www.imdb.com/name/${v}`,
  tmdb: v => `https://www.themoviedb.org/person/${v}`,
  wikidata: v => `https://www.wikidata.org/wiki/${v}`,
}
const identityLinks = computed(() =>
  (data.value?.external_ids ?? [])
    .map(ext => ({ label: formatKey(ext.provider || ''), url: ext.provider && ext.value ? PROVIDER_URL[ext.provider]?.(ext.value) : undefined }))
    .filter(link => link.url),
)
const homepage = computed(() => formatValue(info.value.homepage))
</script>

<template>
  <div class="shell detail-page">
    <NuxtLink to="/browse" class="back-link">← Browse library</NuxtLink>

    <EmptyState v-if="!data && !pending" title="Person unavailable." :message="error || 'This person could not be loaded.'" />

    <template v-else-if="data">
      <header class="person-hero">
        <div class="person-hero__art">
          <MetadataImage :image-id="image" :alt="name" variant="hero" />
        </div>
        <div class="person-hero__body">
          <p class="hero__kicker">
            <span>Person</span>
            <template v-if="knownFor"><i aria-hidden="true" />{{ knownFor }}</template>
            <template v-if="enriching"><i aria-hidden="true" /><em>enriching…</em></template>
          </p>
          <h1 class="editorial person-hero__name">{{ name }}</h1>
          <p class="person-hero__count">{{ creditTotal }} credited titles · combined across providers</p>
          <p v-if="aliases.length" class="person-hero__aliases">Also credited as {{ aliases.join(', ') }}</p>
        </div>
      </header>

      <div class="overview-grid">
        <OverviewPanel title="Personal facts" kicker="Identity">
          <FactList :facts="facts" />
        </OverviewPanel>

        <OverviewPanel v-if="identityLinks.length || homepage" title="Identity" kicker="Off-platform">
          <div class="chip-row">
            <a v-for="link in identityLinks" :key="link.label" :href="link.url" target="_blank" rel="noopener noreferrer" class="chip chip--accent">{{ link.label }} ↗</a>
            <a v-if="homepage" :href="homepage" target="_blank" rel="noopener noreferrer" class="chip chip--accent">Homepage ↗</a>
          </div>
        </OverviewPanel>

        <OverviewPanel v-if="biography" title="Biography" kicker="About" full>
          <p class="person-bio" :class="{ 'is-clamped': bioLong && !bioExpanded }">{{ biography }}</p>
          <button v-if="bioLong" type="button" class="btn--link person-bio__more" @click="bioExpanded = !bioExpanded">
            {{ bioExpanded ? 'Show less' : 'Read more' }}
          </button>
        </OverviewPanel>
      </div>

      <PersonFilmography :credits="credits" />
    </template>
  </div>
</template>

<style scoped>
.person-hero {
  display: grid;
  grid-template-columns: minmax(9rem, 13rem) 1fr;
  gap: clamp(1.5rem, 4vw, 3rem);
  align-items: center;
  padding-top: 1rem;
  margin-bottom: clamp(1.5rem, 3vw, 2.25rem);
}
.person-hero__art { aspect-ratio: 3 / 4; overflow: hidden; border: 1px solid var(--line-strong); border-radius: var(--radius); }
.person-hero__name { margin: 0.5rem 0 0.6rem; font-size: clamp(2rem, 4vw, 3.4rem); }
.person-hero__count { margin: 0; color: var(--muted); font-size: 0.82rem; }
.person-hero__aliases { margin: 0.6rem 0 0; color: var(--muted-2); font-size: 0.76rem; }
.hero__kicker { display: flex; align-items: center; gap: 0.6rem; margin: 0; color: #8b9697; font-family: var(--font-mono); font-size: 0.64rem; letter-spacing: 0.1em; text-transform: uppercase; }
.hero__kicker span { color: var(--gold); }
.hero__kicker i { width: 0.25rem; height: 0.25rem; border-radius: 50%; background: #536064; }
.hero__kicker em { color: var(--green); font-style: normal; }
.person-bio { max-width: 60rem; margin: 0; color: #a0aaa7; font-size: 0.84rem; line-height: 1.75; }
.person-bio.is-clamped { display: -webkit-box; overflow: hidden; -webkit-box-orient: vertical; -webkit-line-clamp: 4; }
.person-bio__more { margin-top: 0.6rem; color: var(--gold); }

@media (max-width: 640px) { .person-hero { grid-template-columns: 1fr; } .person-hero__art { width: min(11rem, 50vw); } }
</style>
