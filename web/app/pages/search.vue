<script setup lang="ts">
import { kindMeta } from '~/utils/kinds'
import type { DiscoveryCandidate } from '~/utils/types'

useSeoMeta({
  title: 'Search',
  description: 'Search what Heya already knows, inspect upstream candidates, resolve the right identity, and audit the result.',
  twitterCard: 'summary_large_image',
})

// Search workbench. q + kind live in the URL and drive the local canonical
// search (restored on reload/back/forward). Upstream discovery and resolution
// are explicit, side-effecting actions kept in memory — never auto-run from a URL.
const route = useRoute()
const api = useHeyaApi()
const { signature } = useLocale()

const q = computed(() => ((route.query.q as string) || '').trim())
const kind = computed(() => (route.query.kind as string) || '')
const kindConfig = computed(() => kindMeta(kind.value))
const canDiscover = computed(() => !!kindConfig.value?.discoverable)
const localeSignature = computed(signature)

// Lazy: the hero copy, search box, and kind selector are static and should
// paint immediately; the template already renders a pending skeleton.
const { data: searchData, pending } = useLazyAsyncData(
  () => `search:${q.value}:${kind.value}:${localeSignature.value}`,
  () => (q.value ? api.search(q.value, kind.value, 30) : Promise.resolve({ results: [] })),
  { default: () => ({ results: [] }), getCachedData: sessionCached },
)
const results = computed(() => searchData.value?.results ?? [])

// ---- Discovery / resolution (in-memory) ----------------------------------
// Provider-transparent flow: discovery returns either a unique entity_id (go
// straight there) or needs_selection candidates. Selection posts only the
// opaque candidate_ref. No provider identity is ever displayed or constructed.
const discovering = ref(false)
const discoveryError = ref('')
const candidates = ref<DiscoveryCandidate[]>([])
const hasCandidateArtwork = computed(() => candidates.value.some(candidate => !!candidate.display?.image_url))
const resolvingRef = ref('')
// Kind reported by discovery, used only to pick the canonical detail route.
const resolvedKind = ref('')

// Reset discovery when the query changes.
watch([q, kind], () => { candidates.value = []; discoveryError.value = ''; resolvedKind.value = '' })

async function runDiscovery() {
  if (!q.value || !canDiscover.value || discovering.value) return
  discovering.value = true
  discoveryError.value = ''
  candidates.value = []
  try {
    let discovery = await api.createDiscovery({ kind: kind.value, query: q.value, limit: 12 })
    if (discovery.state !== 'completed' && discovery.state !== 'failed') {
      discovery = await api.pollDiscovery(discovery.id)
    }
    if (discovery.state === 'failed') throw new Error(discovery.error || 'Upstream discovery failed')
    const result = discovery.result ?? {}
    resolvedKind.value = result.kind || kind.value
    // Unique identity → straight to the canonical entity, no selection needed.
    if (result.entity_id) {
      await navigateTo(entityPath({ id: result.entity_id, kind: resolvedKind.value }))
      return
    }
    if (result.status === 'needs_selection') {
      candidates.value = result.candidates ?? []
      if (!candidates.value.length) discoveryError.value = 'Discovery found no candidates to choose from.'
    } else {
      discoveryError.value = 'Discovery finished without a canonical match.'
    }
  } catch (reason: any) {
    discoveryError.value = reason?.message || 'Discovery failed'
  } finally {
    discovering.value = false
  }
}

function candidateSubtitle(candidate: DiscoveryCandidate) {
  const display = candidate.display ?? {}
  let activeDates = ''
  if (display.begin_date) {
    activeDates = display.end_date
      ? `${display.begin_date}–${display.end_date}`
      : display.ended ? display.begin_date : `${display.begin_date}–present`
  }
  return [
    display.year,
    titleCase(display.type),
    display.area || display.country,
    activeDates,
    formatValue(display.artists),
  ].filter(Boolean).join(' · ')
}

function candidateAliases(candidate: DiscoveryCandidate) {
  const name = candidate.display?.name?.toLocaleLowerCase()
  return (candidate.display?.aliases ?? [])
    .filter(alias => alias.toLocaleLowerCase() !== name)
    .slice(0, 3)
    .join(', ')
}

function candidateMetrics(candidate: DiscoveryCandidate) {
  const display = candidate.display ?? {}
  return [
    display.release_count ? `${formatCount(display.release_count)} releases` : '',
    display.fan_count ? `${formatCount(display.fan_count)} fans` : '',
  ].filter(Boolean).join(' · ')
}

function candidateInitial(candidate: DiscoveryCandidate) {
  return (candidate.display?.name || candidate.display?.title || '?').trim().charAt(0).toLocaleUpperCase()
}

async function resolveCandidate(candidate: DiscoveryCandidate) {
  if (resolvingRef.value) return
  resolvingRef.value = candidate.candidate_ref
  discoveryError.value = ''
  try {
    const result = await api.createResolution({ candidate_ref: candidate.candidate_ref })
    let entityId: string | undefined = result.entity_id
    if (!entityId) {
      if (!result.job?.id) throw new Error('Resolution returned no entity or job')
      const job = await api.pollJob(result.job.id)
      entityId = job.entity_id
    }
    if (!entityId) throw new Error('Ingestion completed without an entity ID')
    await navigateTo(entityPath({ id: entityId, kind: resolvedKind.value || kind.value }))
  } catch (reason: any) {
    discoveryError.value = reason?.message || 'Resolution failed'
  } finally {
    resolvingRef.value = ''
  }
}
</script>

<template>
  <div class="shell page search-page">
    <div class="search-intro">
      <span class="section-label">Canonical metadata, under a microscope</span>
      <h1 class="editorial">Find the thing.<br><em>See the whole story.</em></h1>
      <p>Search what Heya already knows, inspect upstream candidates, resolve the right identity, and audit the result.</p>
    </div>

    <GlobalSearch class="search-box" size="hero" :initial-query="q" :initial-kind="kind" />

    <template v-if="q">
      <header class="results-header">
        <div>
          <span class="section-label">Canonical library</span>
          <h2>{{ results.length }} known {{ results.length === 1 ? 'entity' : 'entities' }} for “{{ q }}”</h2>
        </div>
        <button v-if="canDiscover" type="button" class="btn" :disabled="discovering" @click="runDiscovery">
          {{ candidates.length ? 'Run discovery again' : 'Search upstream providers' }}
        </button>
      </header>

      <div v-if="discoveryError" class="notice"><strong>That didn't work.</strong><span>{{ discoveryError }}</span><button @click="discoveryError = ''">×</button></div>
      <div v-if="pending || discovering" class="progress-line">
        <span class="spinner" /><p>{{ discovering ? 'Asking upstream providers…' : 'Searching the canonical library…' }}</p>
      </div>

      <MediaGrid v-if="results.length && !discovering" :shape="'poster'">
        <MediaCard v-for="item in results" :key="item.id" :entity="item" :shape="cardShape(item.kind)" />
      </MediaGrid>

      <section v-if="candidates.length" class="candidates">
        <h3 class="candidates__title">{{ candidates.length }} candidates need selection</h3>
        <article v-for="candidate in candidates" :key="candidate.candidate_ref" class="candidate" :class="{ 'candidate--with-art': hasCandidateArtwork }">
          <div class="candidate__rank">{{ String(candidate.rank).padStart(2, '0') }}</div>
          <div v-if="hasCandidateArtwork" class="candidate__art" :class="{ 'candidate__art--empty': !candidate.display.image_url }">
            <img
              v-if="candidate.display.image_url"
              :src="candidate.display.image_url"
              :alt="candidate.display.name || candidate.display.title || 'Candidate artwork'"
              loading="lazy"
              referrerpolicy="no-referrer"
            >
            <span v-else>{{ candidateInitial(candidate) }}</span>
          </div>
          <div class="candidate__main">
            <div class="candidate__head">
              <h4>{{ formatValue(candidate.display.title || candidate.display.name) || 'Untitled' }}</h4>
            </div>
            <p v-if="candidateSubtitle(candidate)" class="candidate__sub">{{ candidateSubtitle(candidate) }}</p>
            <p v-if="candidate.display.disambiguation" class="candidate__description">{{ candidate.display.disambiguation }}</p>
            <div v-if="candidate.display.genres?.length" class="candidate__genres">
              <span v-for="genre in candidate.display.genres.slice(0, 6)" :key="genre">{{ genre }}</span>
            </div>
            <p v-if="candidateAliases(candidate)" class="candidate__detail">Also known as {{ candidateAliases(candidate) }}</p>
            <p v-if="candidateMetrics(candidate)" class="candidate__detail">{{ candidateMetrics(candidate) }}</p>
            <div v-if="candidate.evidence?.length" class="candidate__evidence">
              <span v-for="(fact, index) in candidate.evidence.slice(0, 4)" :key="index">
                <i :class="{ negative: fact.weight < 0 }" />{{ formatKey(fact.field) }}: {{ formatValue(fact.outcome) }}
              </span>
            </div>
          </div>
          <div class="candidate__confidence">
            <strong>{{ percent(candidate.confidence) }}</strong>
            <span>{{ candidate.match || 'candidate' }}</span>
          </div>
          <button type="button" class="btn btn--green" :disabled="resolvingRef === candidate.candidate_ref" @click="resolveCandidate(candidate)">
            {{ resolvingRef === candidate.candidate_ref ? 'Building…' : 'Resolve' }}
          </button>
        </article>
      </section>

      <EmptyState
        v-else-if="!results.length && !pending && !discovering"
        title="Nothing canonical yet."
        :message="canDiscover ? 'That is a useful answer. Search upstream providers to find and resolve the right identity.' : 'Choose a specific domain to ask upstream providers.'"
      >
        <button v-if="canDiscover" type="button" class="btn btn--gold" @click="runDiscovery">Discover {{ kindConfig?.label.toLowerCase() }}</button>
      </EmptyState>
    </template>

    <EmptyState v-else title="Start with a search." message="Search the canonical library above, then optionally reach upstream to resolve a new identity." />
  </div>
</template>

<style scoped>
.search-intro { max-width: 52rem; }
.search-intro h1 { margin: 1rem 0 1.1rem; font-size: clamp(2.6rem, 5.5vw, 4.8rem); }
.search-intro p { max-width: 40rem; color: var(--muted); font-size: 0.95rem; line-height: 1.7; }
.search-box { max-width: 60rem; margin-top: 2.5rem; }

.results-header {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 1.5rem;
  margin: clamp(2.5rem, 5vw, 4rem) 0 1.5rem;
  padding-bottom: 1rem;
  border-bottom: 1px solid var(--line);
}
.results-header h2 { margin: 0.4rem 0 0; font-size: 1.3rem; font-weight: 500; }

.candidates { margin-top: 2.5rem; border-top: 1px solid var(--line); }
.candidates__title { margin: 1.5rem 0; font-size: 0.9rem; font-weight: 500; }
.candidate {
  display: grid;
  grid-template-columns: 2.5rem 1fr auto auto;
  align-items: center;
  gap: 1.5rem;
  padding: 1.25rem 0.25rem;
  border-bottom: 1px solid var(--line);
}
.candidate--with-art { grid-template-columns: 2.5rem 5rem 1fr auto auto; }
.candidate__rank { color: #4d585d; font-family: var(--font-mono); font-size: 0.72rem; }
.candidate__art {
  overflow: hidden;
  width: 5rem;
  aspect-ratio: 1;
  border: 1px solid var(--line);
  border-radius: 0.25rem;
  background: #15191b;
}
.candidate__art img { width: 100%; height: 100%; object-fit: cover; }
.candidate__art--empty { display: grid; place-items: center; color: #536066; font-family: var(--font-mono); font-size: 1.4rem; }
.candidate__main { min-width: 0; }
.candidate__head { display: flex; align-items: baseline; gap: 0.8rem; }
.candidate__provider { color: var(--gold); font-family: var(--font-mono); font-size: 0.58rem; text-transform: uppercase; }
.candidate__head h4 { overflow: hidden; margin: 0; font-size: 1rem; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.candidate__sub { margin: 0.2rem 0 0; color: var(--muted); font-size: 0.7rem; }
.candidate__description { margin: 0.35rem 0 0; color: #b6c0c4; font-size: 0.72rem; line-height: 1.4; }
.candidate__genres { display: flex; flex-wrap: wrap; gap: 0.35rem; margin-top: 0.55rem; }
.candidate__genres span { padding: 0.18rem 0.4rem; border: 1px solid #354045; border-radius: 999px; color: #9eaaaf; font-size: 0.58rem; }
.candidate__detail { margin: 0.35rem 0 0; color: #768187; font-size: 0.62rem; line-height: 1.4; }
.candidate__evidence { display: flex; flex-wrap: wrap; gap: 1rem; margin-top: 0.55rem; color: #768187; font-size: 0.62rem; }
.candidate__evidence span { display: flex; align-items: center; gap: 0.35rem; }
.candidate__evidence i { width: 0.3rem; height: 0.3rem; border-radius: 50%; background: var(--green); }
.candidate__evidence i.negative { background: var(--danger); }
.candidate__confidence { min-width: 4rem; text-align: right; }
.candidate__confidence strong { display: block; font-family: var(--font-mono); font-size: 0.95rem; }
.candidate__confidence span { color: var(--muted-2); font-size: 0.55rem; text-transform: uppercase; }

@media (max-width: 720px) {
  .candidate { grid-template-columns: 2rem 1fr; gap: 0.9rem; }
  .candidate--with-art { grid-template-columns: 2rem 4rem 1fr; }
  .candidate__art { width: 4rem; }
  .candidate__confidence { display: none; }
  .candidate > .btn { grid-column: 2 / -1; justify-self: start; }
  .candidate--with-art > .btn { grid-column: 3 / -1; }
}
</style>
