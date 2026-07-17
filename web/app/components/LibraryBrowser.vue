<script setup lang="ts">
import { BROWSE_CATEGORIES, cardShape, kindLabel, kindPlural } from '~/utils/kinds'
import type { CardShape } from '~/utils/kinds'
import type { LibraryStats } from '~/utils/types'

// URL-query-driven view used by /browse (kind selectable) and the domain
// listings (kind locked). Three states, all restorable from the query:
//   • landing  — no kind, no search: a grid of category cards (browse by domain)
//   • grid     — a kind, `all=1`, or a search: the flat entity grid + pagination
//   • locked   — domain pages pass lockKind: always the grid, no switcher
// kind, sort, offset, all, and q live in the query so back/forward restore
// everything and the geometry survives loading/empty/error.
const props = withDefaults(defineProps<{
  kind?: string
  lockKind?: boolean
  title: string
  kicker?: string
}>(), { kind: '', lockKind: false, kicker: 'Canonical index' })

const LIMIT = 24
const api = useHeyaApi()
const { signature } = useLocale()
const route = useRoute()
const localeSignature = computed(signature)

const effectiveKind = computed(() => (props.lockKind ? props.kind : ((route.query.kind as string) || '')))
const sort = computed(() => (route.query.sort as string) || 'updated')
const offset = computed(() => Math.max(0, Number.parseInt(route.query.offset as string) || 0))
const q = computed(() => (props.lockKind ? '' : ((route.query.q as string) || '')))
const allMode = computed(() => !props.lockKind && route.query.all === '1')
const localQuery = ref(q.value)
watch(q, value => { localQuery.value = value })

// The category landing shows only when nothing narrows the view.
const showLanding = computed(() => !props.lockKind && !effectiveKind.value && !allMode.value && !q.value)
const gridShape = computed<CardShape>(() => (props.lockKind && props.kind ? cardShape(props.kind) : 'poster'))

// ---- Category cards (browse landing) --------------------------------------
// One stats call for live counts plus a shallow latest() per domain for the
// collage. Skipped entirely on locked domain pages.
const { data: catData, pending: catPending } = await useAsyncData(
  () => `browse-categories:${props.lockKind ? props.kind : 'all'}:${localeSignature.value}`,
  async () => {
    if (props.lockKind) return { counts: {} as Record<string, number>, samples: {} as Record<string, Array<string | undefined>>, total: 0 }
    const [stats, ...lists] = await Promise.all([
      api.stats().catch(() => ({} as LibraryStats)),
      ...BROWSE_CATEGORIES.map(kind => api.latest(kind, 4).then(r => r.results ?? []).catch(() => [])),
    ])
    const samples: Record<string, Array<string | undefined>> = {}
    BROWSE_CATEGORIES.forEach((kind, index) => { samples[kind] = lists[index].map(entityImageId) })
    return { counts: (stats.kinds ?? {}) as Record<string, number>, samples, total: Number(stats.entities ?? 0) }
  },
  { default: () => ({ counts: {} as Record<string, number>, samples: {} as Record<string, Array<string | undefined>>, total: 0 }), getCachedData: sessionCached },
)

const categories = computed(() =>
  BROWSE_CATEGORIES
    .map(kind => {
      const count = Number(catData.value?.counts?.[kind] ?? 0)
      return {
        kind,
        label: kindPlural(kind),
        noun: (count === 1 ? kindLabel(kind) : kindPlural(kind)).toLowerCase(),
        shape: cardShape(kind),
        count,
        samples: catData.value?.samples?.[kind] ?? [],
      }
    })
    .filter(category => category.count > 0),
)
const totalEntities = computed(() => catData.value?.total ?? 0)

// ---- Flat grid ------------------------------------------------------------
// The key carries every query input, so each page/sort/filter combination gets
// its own session-cached slot and back/forward restores instantly.
const { data, pending, error } = await useAsyncData(
  () => `browse:${effectiveKind.value || 'all'}:${sort.value}:${offset.value}:${q.value}:${localeSignature.value}`,
  () => api.browse({ kind: effectiveKind.value, sort: sort.value, offset: offset.value, limit: LIMIT, q: q.value }),
  { default: () => ({ results: [], total: 0, offset: 0, limit: LIMIT }), getCachedData: sessionCached },
)

const results = computed(() => data.value?.results ?? [])
const total = computed(() => data.value?.total ?? 0)
const page = computed(() => Math.floor(offset.value / LIMIT) + 1)
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / LIMIT)))
const rangeEnd = computed(() => Math.min(offset.value + LIMIT, total.value))

// Heading subline reflects whichever view is active.
const activeCategoryLabel = computed(() => (effectiveKind.value ? kindPlural(effectiveKind.value) : ''))

function patchQuery(patch: Record<string, string | undefined>, resetOffset = true) {
  const query: Record<string, any> = { ...route.query, ...patch }
  if (resetOffset) delete query.offset
  for (const key of Object.keys(query)) {
    if (query[key] === '' || query[key] == null) delete query[key]
  }
  navigateTo({ path: route.path, query })
}

function goToPage(delta: number) {
  const next = Math.max(0, offset.value + delta * LIMIT)
  patchQuery({ offset: next ? String(next) : undefined }, false)
}

function applySearch() {
  patchQuery({ q: localQuery.value.trim() || undefined, all: undefined })
}

function selectKind(kind: string) {
  patchQuery({ kind: kind || undefined, all: undefined })
}

function selectAll() {
  patchQuery({ kind: undefined, all: '1' })
}
</script>

<template>
  <div class="shell page">
    <header class="page-heading">
      <div>
        <span class="section-label">{{ kicker }}</span>
        <h1>{{ title }}</h1>
        <p v-if="showLanding && totalEntities">Pick a domain, or search across all {{ formatCount(totalEntities) }} entities.</p>
        <p v-else-if="activeCategoryLabel && total">{{ activeCategoryLabel }} · {{ formatCount(total) }} · showing {{ offset + 1 }}–{{ rangeEnd }}</p>
        <p v-else-if="total">{{ formatCount(total) }} entities · showing {{ offset + 1 }}–{{ rangeEnd }}</p>
      </div>
    </header>

    <!-- ============ Landing: browse by category ============ -->
    <template v-if="showLanding">
      <form class="browse-landing__search" @submit.prevent="applySearch">
        <input v-model="localQuery" type="search" placeholder="Search every domain by title…" aria-label="Search all entities by title">
        <button type="submit" class="btn btn--sm">Search</button>
        <button type="button" class="btn btn--sm btn--ghost" @click="selectAll">Browse everything →</button>
      </form>

      <LoadingSkeleton v-if="catPending" layout="grid" shape="landscape" :count="8" />
      <div v-else-if="categories.length" class="category-grid">
        <CategoryCard
          v-for="category in categories"
          :key="category.kind"
          :label="category.label"
          :kind="category.kind"
          :count="category.count"
          :noun="category.noun"
          :shape="category.shape"
          :samples="category.samples"
        />
      </div>
      <EmptyState
        v-else
        title="The library is empty."
        message="Resolve an entity from the search workbench to populate the canonical library."
      />
    </template>

    <!-- ============ Grid: a kind, everything, or a search ============ -->
    <template v-else>
      <nav v-if="!lockKind" class="browse-switch" aria-label="Browse categories">
        <NuxtLink to="/browse" class="browse-switch__back">← Categories</NuxtLink>
        <div class="chips">
          <button type="button" class="chip" :class="{ 'is-active': allMode }" @click="selectAll">Everything</button>
          <button
            v-for="category in categories"
            :key="category.kind"
            type="button"
            class="chip"
            :class="{ 'is-active': effectiveKind === category.kind }"
            @click="selectKind(category.kind)"
          >{{ category.label }} <i>{{ formatCount(category.count) }}</i></button>
        </div>
      </nav>

      <div class="browse-controls">
        <label class="browse-control">
          <span class="sr-only">Sort</span>
          <select :value="sort" @change="patchQuery({ sort: ($event.target as HTMLSelectElement).value })">
            <option value="updated">Recently updated</option>
            <option value="title">Title A–Z</option>
            <option value="year">Newest release</option>
            <option value="popular">Provider popularity</option>
          </select>
        </label>

        <form v-if="!lockKind" class="browse-search" @submit.prevent="applySearch">
          <input v-model="localQuery" type="search" placeholder="Filter by title…" aria-label="Filter results by title">
          <button type="submit" class="btn btn--sm">Filter</button>
        </form>
      </div>

      <div v-if="error" class="notice"><strong>That didn't work.</strong><span>{{ error }}</span></div>

      <LoadingSkeleton v-if="pending" layout="grid" :shape="gridShape" :count="12" />

      <MediaGrid v-else-if="results.length" :shape="gridShape">
        <MediaCard v-for="item in results" :key="item.id" :entity="item" :shape="lockKind ? gridShape : cardShape(item.kind)" />
      </MediaGrid>

      <EmptyState
        v-else
        title="Nothing here yet."
        :message="q ? `No entities match “${q}”.` : 'No entities in this view. Resolve one from the search workbench.'"
      />

      <nav v-if="pageCount > 1" class="pagination" aria-label="Pagination">
        <button type="button" class="btn btn--sm" :disabled="offset === 0" @click="goToPage(-1)">← Previous</button>
        <span>Page {{ page }} of {{ pageCount }}</span>
        <button type="button" class="btn btn--sm" :disabled="rangeEnd >= total" @click="goToPage(1)">Next →</button>
      </nav>
    </template>
  </div>
</template>

<style scoped>
/* ---- Category landing ---- */
.category-grid {
  display: grid;
  gap: 1rem;
  grid-template-columns: repeat(auto-fill, minmax(248px, 1fr));
}
.browse-landing__search { display: flex; flex-wrap: wrap; gap: 0.5rem; margin-bottom: 1.5rem; }
.browse-landing__search input {
  flex: 1 1 22rem;
  padding: 0.6rem 0.8rem;
  border: 1px solid var(--line-strong);
  border-radius: var(--radius-sm);
  background: var(--panel);
  color: var(--text);
  font-size: 0.72rem;
}
.browse-landing__search input:focus { outline: none; border-color: #6f643e; }

/* ---- Category switcher (grid view) ---- */
.browse-switch { display: flex; align-items: center; gap: 1rem; margin-bottom: 1.1rem; }
.browse-switch__back { flex: none; color: var(--muted); font-size: 0.72rem; white-space: nowrap; }
.browse-switch__back:hover { color: var(--gold); }
.chips {
  display: flex;
  gap: 0.4rem;
  overflow-x: auto;
  padding-bottom: 0.35rem;
  scrollbar-width: thin;
}
.chip {
  flex: none;
  padding: 0.4rem 0.7rem;
  border: 1px solid var(--line-strong);
  border-radius: 2rem;
  background: var(--panel);
  color: var(--text-dim);
  font-size: 0.7rem;
  white-space: nowrap;
  cursor: pointer;
  transition: border-color 0.15s ease, color 0.15s ease, background 0.15s ease;
}
.chip:hover { border-color: #6f643e; color: var(--text); }
.chip i { color: var(--muted-2); font-style: normal; font-family: var(--font-mono); font-size: 0.62rem; }
.chip.is-active { border-color: var(--gold); background: rgba(241, 201, 107, 0.12); color: var(--gold); }
.chip.is-active i { color: var(--gold-strong); }

/* ---- Shared grid controls ---- */
.browse-controls { display: flex; flex-wrap: wrap; gap: 0.6rem; margin-bottom: 1.5rem; }
.browse-control select {
  padding: 0.6rem 2.2rem 0.6rem 0.8rem;
  border: 1px solid var(--line-strong);
  border-radius: var(--radius-sm);
  background: var(--panel);
  color: #b8c0bd;
  font-size: 0.72rem;
}
.browse-search { display: flex; gap: 0.5rem; margin-left: auto; }
.browse-search input {
  padding: 0.6rem 0.8rem;
  border: 1px solid var(--line-strong);
  border-radius: var(--radius-sm);
  background: var(--panel);
  color: var(--text);
  font-size: 0.72rem;
}
.browse-search input:focus { outline: none; border-color: #6f643e; }
.pagination { display: flex; justify-content: center; align-items: center; gap: 1rem; margin-top: 2rem; color: var(--muted-2); font-size: 0.7rem; }

@media (max-width: 560px) {
  .browse-search { width: 100%; margin-left: 0; }
  .browse-search input { flex: 1 1 auto; }
  .browse-landing__search input { flex-basis: 100%; }
}
</style>
