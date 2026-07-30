<script setup lang="ts">
import { kindMeta } from '~/utils/kinds'
import type { DiscoveryCandidate } from '~/utils/types'
import {
  candidateAliases,
  candidateConfidencePercent,
  candidateEvidence,
  candidateEvidenceSummary,
  candidateFacts,
  candidateHasIdentityDetails,
  candidateInitial,
  candidateKind,
  candidateName,
} from '~/utils/discoveryCandidate'

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
const hasConvergedCandidate = computed(() => candidates.value.length === 1 && candidates.value[0]?.evidence?.some(fact => fact.field === 'identity_crosswalk'))
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
          <span class="section-label">Search results</span>
          <h2>Results for “{{ q }}”</h2>
          <p>{{ results.length }} possible existing {{ results.length === 1 ? 'match' : 'matches' }} in the canonical library</p>
        </div>
        <button v-if="canDiscover" type="button" class="btn" :disabled="discovering" @click="runDiscovery">
          {{ candidates.length ? 'Refresh upstream results' : 'Search upstream providers' }}
        </button>
      </header>

      <div v-if="discoveryError" class="notice"><strong>That didn't work.</strong><span>{{ discoveryError }}</span><button @click="discoveryError = ''">×</button></div>
      <div v-if="pending || discovering" class="progress-line">
        <span class="spinner" /><p>{{ discovering ? 'Asking upstream providers…' : 'Searching the canonical library…' }}</p>
      </div>

      <section v-if="candidates.length" class="identity-review" aria-labelledby="identity-review-title">
        <header class="identity-review__header">
          <div>
            <span class="section-label">Identity review</span>
            <h2 id="identity-review-title">{{ candidates.length === 1 ? `Review the identity matched for “${q}”` : `Choose the “${q}” you meant` }}</h2>
            <p v-if="candidates.length === 1">The available identity evidence now points to one upstream record. Review its combined details before choosing it.</p>
            <p v-else>These upstream records share a name, but Heya cannot safely prove they are the same identity. Compare their artwork and identifying details before choosing.</p>
          </div>
          <div class="identity-review__count">
            <strong>{{ candidates.length }}</strong>
            <span>{{ candidates.length === 1 ? 'matched identity' : 'possible identities' }}</span>
          </div>
        </header>

        <div class="identity-review__explanation">
          <strong>{{ hasConvergedCandidate ? 'Sources already converged' : candidates.length === 1 ? 'Why review it?' : 'Why are there several?' }}</strong>
          <span v-if="hasConvergedCandidate">Authoritative cross-links joined the matching upstream records into this single identity.</span>
          <span v-else-if="candidates.length === 1">One upstream identity matched, but Heya still leaves the final identity choice to you.</span>
          <span v-else>Each card is a separate upstream identity. Heya keeps them apart until stronger evidence proves they converge.</span>
        </div>

        <div class="candidate-grid" :class="{ 'candidate-grid--single': candidates.length === 1, 'candidate-grid--pair': candidates.length === 2 }">
          <article v-for="candidate in candidates" :key="candidate.candidate_ref" class="candidate">
            <div class="candidate__art" :class="{ 'candidate__art--empty': !candidate.display.image_url }">
              <span class="candidate__rank">Candidate {{ String(candidate.rank).padStart(2, '0') }}</span>
              <img
                v-if="candidate.display.image_url"
                :src="candidate.display.image_url"
                :alt="`${candidateName(candidate)} artwork`"
                loading="lazy"
                referrerpolicy="no-referrer"
              >
              <span v-else>{{ candidateInitial(candidate) }}</span>
            </div>

            <div class="candidate__body">
              <header class="candidate__head">
                <span>{{ candidateKind(candidate, kindConfig?.label) }}</span>
                <h3>{{ candidateName(candidate) }}</h3>
                <p v-if="candidate.display.disambiguation">{{ candidate.display.disambiguation }}</p>
              </header>

              <dl v-if="candidateFacts(candidate).length" class="candidate__facts">
                <div v-for="fact in candidateFacts(candidate)" :key="fact.label">
                  <dt>{{ fact.label }}</dt>
                  <dd>{{ fact.value }}</dd>
                </div>
              </dl>

              <div v-if="candidate.display.genres?.length" class="candidate__genres" aria-label="Genres">
                <span v-for="genre in candidate.display.genres.slice(0, 6)" :key="genre">{{ genre }}</span>
              </div>

              <p v-if="candidateAliases(candidate).length" class="candidate__aliases">
                <span>Also known as</span>
                {{ candidateAliases(candidate).join(', ') }}
              </p>

              <p v-if="!candidateHasIdentityDetails(candidate)" class="candidate__empty-context">
                No additional identifying details were supplied for this record.
              </p>

              <div v-if="candidateEvidence(candidate).length" class="candidate__evidence">
                <span class="candidate__evidence-label">Why it matched</span>
                <div>
                  <span
                    v-for="(fact, index) in candidateEvidence(candidate)"
                    :key="index"
                    :class="`candidate__evidence-item--${fact.tone}`"
                    class="candidate__evidence-item"
                  >
                    <i />{{ fact.label }}
                  </span>
                </div>
              </div>

              <div class="candidate__decision">
                <div class="candidate__score">
                  <div>
                    <span>Match evidence</span>
                    <strong>{{ candidateConfidencePercent(candidate) }}/100</strong>
                  </div>
                  <div class="candidate__score-track" role="progressbar" aria-label="Match evidence" aria-valuemin="0" aria-valuemax="100" :aria-valuenow="candidateConfidencePercent(candidate)">
                    <span :style="{ width: `${candidateConfidencePercent(candidate)}%` }" />
                  </div>
                  <p>{{ candidateEvidenceSummary(candidate) }}. This is a ranking signal, not proof of identity.</p>
                </div>

                <button
                  type="button"
                  class="btn btn--green candidate__choose"
                  :disabled="!!resolvingRef"
                  :aria-label="`Choose ${candidateName(candidate)}`"
                  @click="resolveCandidate(candidate)"
                >
                  {{ resolvingRef === candidate.candidate_ref ? 'Creating identity…' : 'Choose this identity' }}
                </button>
              </div>
            </div>
          </article>
        </div>

        <p class="identity-review__footnote">Choosing an identity creates or opens its canonical Heya record. The other records remain separate unless later evidence proves they refer to the same identity.</p>
      </section>

      <section v-if="results.length && !discovering" class="library-results">
        <header class="library-results__header">
          <div>
            <span class="section-label">Canonical library</span>
            <h2>{{ results.length }} possible existing {{ results.length === 1 ? 'match' : 'matches' }}</h2>
          </div>
          <p>These are fuzzy name or title matches already known to Heya. They are not confirmed as the identity you searched for.</p>
        </header>
        <MediaGrid :shape="'poster'">
          <MediaCard v-for="item in results" :key="item.id" :entity="item" :shape="cardShape(item.kind)" />
        </MediaGrid>
      </section>

      <EmptyState
        v-if="!results.length && !candidates.length && !pending && !discovering"
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
.results-header p { margin: 0.45rem 0 0; color: var(--muted); font-size: 0.72rem; }

.identity-review {
  margin-top: 2rem;
  padding: clamp(1.25rem, 2.5vw, 2rem);
  border: 1px solid #3a4038;
  border-radius: var(--radius);
  background: linear-gradient(145deg, rgba(25, 30, 26, 0.9), rgba(13, 17, 19, 0.96) 45%);
}
.identity-review__header {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 2rem;
}
.identity-review__header h2 { margin: 0.45rem 0 0; font-size: clamp(1.55rem, 2.5vw, 2.2rem); font-weight: 500; letter-spacing: -0.035em; }
.identity-review__header p { max-width: 48rem; margin: 0.65rem 0 0; color: #9ba6a2; font-size: 0.78rem; line-height: 1.6; }
.identity-review__count { flex: 0 0 auto; padding-left: 1.5rem; border-left: 1px solid var(--line-strong); text-align: right; }
.identity-review__count strong { display: block; color: var(--gold); font-family: var(--font-mono); font-size: 2rem; font-weight: 500; line-height: 1; }
.identity-review__count span { display: block; margin-top: 0.3rem; color: var(--muted); font-size: 0.65rem; white-space: nowrap; }
.identity-review__explanation {
  display: flex;
  gap: 0.65rem;
  margin-top: 1.4rem;
  padding: 0.75rem 0.9rem;
  border-left: 2px solid var(--gold-strong);
  background: rgba(193, 171, 94, 0.06);
  color: #9ba4a1;
  font-size: 0.7rem;
  line-height: 1.5;
}
.identity-review__explanation strong { flex: 0 0 auto; color: #d2c694; font-weight: 500; }
.candidate-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  align-items: stretch;
  gap: 1rem;
  margin-top: 1rem;
}
.candidate-grid.candidate-grid--single { grid-template-columns: minmax(0, 32rem); }
.candidate-grid.candidate-grid--pair { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.candidate {
  display: flex;
  overflow: hidden;
  flex-direction: column;
  min-width: 0;
  border: 1px solid var(--line-strong);
  border-radius: var(--radius);
  background: rgba(11, 15, 17, 0.82);
}
.candidate__art {
  position: relative;
  display: grid;
  place-items: center;
  overflow: hidden;
  width: 100%;
  height: clamp(10rem, 15vw, 13rem);
  border-bottom: 1px solid var(--line);
  background: radial-gradient(circle at center, #252c2d, #121719 68%);
}
.candidate__art img { width: 100%; height: 100%; object-fit: contain; }
.candidate__art--empty { color: #536066; font-family: var(--font-mono); font-size: 3rem; }
.candidate__rank {
  position: absolute;
  z-index: 1;
  top: 0.65rem;
  left: 0.65rem;
  padding: 0.3rem 0.45rem;
  border: 1px solid rgba(126, 137, 138, 0.3);
  border-radius: 999px;
  background: rgba(8, 11, 12, 0.82);
  color: #a5afb0;
  font-family: var(--font-mono);
  font-size: 0.56rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  backdrop-filter: blur(8px);
}
.candidate__body { display: flex; flex: 1; flex-direction: column; min-width: 0; padding: 1rem; }
.candidate__head > span { color: var(--gold); font-family: var(--font-mono); font-size: 0.58rem; letter-spacing: 0.1em; text-transform: uppercase; }
.candidate__head h3 { overflow: hidden; margin: 0.3rem 0 0; font-size: 1.08rem; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.candidate__head p { margin: 0.45rem 0 0; color: #b6c0c4; font-size: 0.7rem; line-height: 1.45; }
.candidate__facts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.7rem 0.9rem;
  margin: 1rem 0 0;
  padding: 0.85rem 0;
  border-block: 1px solid var(--line);
}
.candidate__facts div { min-width: 0; }
.candidate__facts dt { color: #6f7a7d; font-family: var(--font-mono); font-size: 0.55rem; letter-spacing: 0.08em; text-transform: uppercase; }
.candidate__facts dd { overflow: hidden; margin: 0.2rem 0 0; color: #c4cbca; font-size: 0.69rem; line-height: 1.3; text-overflow: ellipsis; }
.candidate__genres { display: flex; flex-wrap: wrap; gap: 0.35rem; margin-top: 0.55rem; }
.candidate__genres span { padding: 0.18rem 0.4rem; border: 1px solid #354045; border-radius: 999px; color: #9eaaaf; font-size: 0.58rem; }
.candidate__aliases { margin: 0.65rem 0 0; color: #8d989b; font-size: 0.65rem; line-height: 1.45; }
.candidate__aliases span { display: block; margin-bottom: 0.15rem; color: #667276; font-family: var(--font-mono); font-size: 0.52rem; letter-spacing: 0.07em; text-transform: uppercase; }
.candidate__empty-context { margin: 1rem 0 0; padding: 0.75rem; border: 1px dashed #30383a; color: #768184; font-size: 0.65rem; line-height: 1.45; }
.candidate__evidence { margin-top: 1rem; }
.candidate__evidence-label { display: block; margin-bottom: 0.45rem; color: #667276; font-family: var(--font-mono); font-size: 0.52rem; letter-spacing: 0.07em; text-transform: uppercase; }
.candidate__evidence > div { display: flex; flex-wrap: wrap; gap: 0.35rem; }
.candidate__evidence-item { display: inline-flex; align-items: center; gap: 0.35rem; padding: 0.24rem 0.4rem; border: 1px solid #30393b; border-radius: 999px; color: #909b9d; font-size: 0.58rem; }
.candidate__evidence-item i { width: 0.3rem; height: 0.3rem; border-radius: 50%; background: #727d80; }
.candidate__evidence-item--positive i { background: var(--green); }
.candidate__evidence-item--negative { border-color: #523c3e; color: #b59194; }
.candidate__evidence-item--negative i { background: var(--danger); }
.candidate__decision { margin-top: auto; padding-top: 1.15rem; }
.candidate__score > div:first-child { display: flex; align-items: baseline; justify-content: space-between; gap: 0.75rem; }
.candidate__score > div:first-child span { color: #788386; font-size: 0.61rem; }
.candidate__score strong { color: #c8cfcd; font-family: var(--font-mono); font-size: 0.72rem; font-weight: 500; }
.candidate__score-track { overflow: hidden; height: 0.22rem; margin-top: 0.45rem; border-radius: 99px; background: #283033; }
.candidate__score-track span { display: block; height: 100%; border-radius: inherit; background: var(--gold-strong); }
.candidate__score p { margin: 0.45rem 0 0; color: #667175; font-size: 0.57rem; line-height: 1.4; }
.candidate__choose { width: 100%; margin-top: 0.85rem; }
.identity-review__footnote { max-width: 52rem; margin: 1rem 0 0; color: #687477; font-size: 0.62rem; line-height: 1.5; }

.library-results { margin-top: clamp(2.5rem, 5vw, 4rem); padding-top: 1.5rem; border-top: 1px solid var(--line); }
.library-results__header { display: flex; align-items: flex-end; justify-content: space-between; gap: 2rem; margin-bottom: 1.25rem; }
.library-results__header h2 { margin: 0.4rem 0 0; font-size: 1.15rem; font-weight: 500; }
.library-results__header p { max-width: 31rem; margin: 0; color: var(--muted); font-size: 0.68rem; line-height: 1.5; text-align: right; }

@media (max-width: 1000px) {
  .candidate-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (max-width: 720px) {
  .results-header, .identity-review__header, .library-results__header { align-items: flex-start; flex-direction: column; }
  .results-header .btn { width: 100%; }
  .identity-review__count { padding: 0; border: 0; text-align: left; }
  .identity-review__explanation { align-items: flex-start; flex-direction: column; }
  .candidate-grid { grid-template-columns: 1fr; }
  .candidate-grid.candidate-grid--pair { grid-template-columns: 1fr; }
  .candidate__art { height: 12rem; }
  .library-results__header { gap: 0.75rem; }
  .library-results__header p { text-align: left; }
}
</style>
