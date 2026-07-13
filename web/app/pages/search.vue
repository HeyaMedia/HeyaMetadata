<script setup lang="ts">
import { kindMeta } from '~/utils/kinds'
import type { DiscoveryCandidate } from '~/utils/types'

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

const { data: searchData, pending } = await useAsyncData(
  'search',
  () => (q.value ? api.search(q.value, kind.value, 30) : Promise.resolve({ results: [] })),
  { watch: [q, kind, localeSignature], default: () => ({ results: [] }) },
)
const results = computed(() => searchData.value?.results ?? [])

// ---- Discovery / resolution (in-memory) ----------------------------------
const discovering = ref(false)
const discoveryError = ref('')
const candidates = ref<DiscoveryCandidate[]>([])
const resolvingKey = ref('')

// Reset discovery when the query changes.
watch([q, kind], () => { candidates.value = []; discoveryError.value = '' })

async function runDiscovery() {
  if (!q.value || !canDiscover.value || discovering.value) return
  discovering.value = true
  discoveryError.value = ''
  candidates.value = []
  try {
    let result = await api.createDiscovery({ kind: kind.value, query: q.value, limit: 12 })
    if (result.state !== 'completed') result = await api.pollDiscovery(result.id)
    if (result.state === 'failed') throw new Error(result.error || 'Upstream discovery failed')
    candidates.value = result.result?.candidates ?? []
  } catch (reason: any) {
    discoveryError.value = reason?.message || 'Discovery failed'
  } finally {
    discovering.value = false
  }
}

function candidateKey(candidate: DiscoveryCandidate) {
  return `${candidate.identity.provider}:${candidate.identity.namespace}:${candidate.identity.value}`
}

function candidateSubtitle(candidate: DiscoveryCandidate) {
  const display = candidate.display ?? {}
  return [display.year, titleCase(display.type), formatValue(display.artists)].filter(Boolean).join(' · ')
}

async function resolveCandidate(candidate: DiscoveryCandidate) {
  const key = candidateKey(candidate)
  if (candidate.existing_entity_id) {
    await navigateTo(entityPath({ id: candidate.existing_entity_id, kind: candidate.resolution.kind }))
    return
  }
  resolvingKey.value = key
  discoveryError.value = ''
  try {
    const result = await api.createResolution(candidate.resolution)
    let entityId: string | undefined = result.entity_id
    if (!entityId) {
      if (!result.job?.id) throw new Error('Resolution returned no entity or job')
      const job = await api.pollJob(result.job.id)
      entityId = job.entity_id
    }
    if (!entityId) throw new Error('Ingestion completed without an entity ID')
    await navigateTo(entityPath({ id: entityId, kind: candidate.resolution.kind }))
  } catch (reason: any) {
    discoveryError.value = reason?.message || 'Resolution failed'
  } finally {
    resolvingKey.value = ''
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
        <h3 class="candidates__title">{{ candidates.length }} upstream candidates</h3>
        <article v-for="candidate in candidates" :key="candidateKey(candidate)" class="candidate">
          <div class="candidate__rank">{{ String(candidate.rank).padStart(2, '0') }}</div>
          <div class="candidate__main">
            <div class="candidate__head">
              <span class="candidate__provider">{{ candidate.identity.provider }}</span>
              <h4>{{ formatValue(candidate.display.title || candidate.display.name) || 'Untitled' }}</h4>
            </div>
            <p v-if="candidateSubtitle(candidate)" class="candidate__sub">{{ candidateSubtitle(candidate) }}</p>
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
          <button type="button" class="btn btn--green" :disabled="resolvingKey === candidateKey(candidate)" @click="resolveCandidate(candidate)">
            {{ resolvingKey === candidateKey(candidate) ? 'Building…' : candidate.existing_entity_id ? 'Open entity' : 'Resolve' }}
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
.candidate__rank { color: #4d585d; font-family: var(--font-mono); font-size: 0.72rem; }
.candidate__main { min-width: 0; }
.candidate__head { display: flex; align-items: baseline; gap: 0.8rem; }
.candidate__provider { color: var(--gold); font-family: var(--font-mono); font-size: 0.58rem; text-transform: uppercase; }
.candidate__head h4 { overflow: hidden; margin: 0; font-size: 1rem; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.candidate__sub { margin: 0.2rem 0 0; color: var(--muted); font-size: 0.7rem; }
.candidate__evidence { display: flex; flex-wrap: wrap; gap: 1rem; margin-top: 0.55rem; color: #768187; font-size: 0.62rem; }
.candidate__evidence span { display: flex; align-items: center; gap: 0.35rem; }
.candidate__evidence i { width: 0.3rem; height: 0.3rem; border-radius: 50%; background: var(--green); }
.candidate__evidence i.negative { background: var(--danger); }
.candidate__confidence { min-width: 4rem; text-align: right; }
.candidate__confidence strong { display: block; font-family: var(--font-mono); font-size: 0.95rem; }
.candidate__confidence span { color: var(--muted-2); font-size: 0.55rem; text-transform: uppercase; }

@media (max-width: 720px) {
  .candidate { grid-template-columns: 2rem 1fr auto; }
  .candidate__confidence { display: none; }
  .candidate > .btn { grid-column: 2 / -1; justify-self: start; }
}
</style>
