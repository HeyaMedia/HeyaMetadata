<script setup lang="ts">
import { FILTER_KINDS, cardShape } from '~/utils/kinds'
import type { CardShape } from '~/utils/kinds'

// URL-query-driven grid used by /browse (kind selectable) and the domain
// listings (kind locked). kind, sort, offset, and optional q all live in the
// query, so back/forward restore filters and the grid geometry is preserved
// across loading/empty/error.
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

const effectiveKind = computed(() => (props.lockKind ? props.kind : ((route.query.kind as string) || '')))
const sort = computed(() => (route.query.sort as string) || 'updated')
const offset = computed(() => Math.max(0, Number.parseInt(route.query.offset as string) || 0))
const q = computed(() => (props.lockKind ? '' : ((route.query.q as string) || '')))
const localQuery = ref(q.value)
const localeSignature = computed(signature)
watch(q, value => { localQuery.value = value })

const gridShape = computed<CardShape>(() => (props.lockKind && props.kind ? cardShape(props.kind) : 'poster'))

const { data, pending, error } = await useAsyncData(
  `browse:${props.lockKind ? props.kind : 'all'}`,
  () => api.browse({ kind: effectiveKind.value, sort: sort.value, offset: offset.value, limit: LIMIT, q: q.value }),
  { watch: [effectiveKind, sort, offset, q, localeSignature], default: () => ({ results: [], total: 0, offset: 0, limit: LIMIT }) },
)

const results = computed(() => data.value?.results ?? [])
const total = computed(() => data.value?.total ?? 0)
const page = computed(() => Math.floor(offset.value / LIMIT) + 1)
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / LIMIT)))
const rangeEnd = computed(() => Math.min(offset.value + LIMIT, total.value))

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
  patchQuery({ q: localQuery.value.trim() || undefined })
}
</script>

<template>
  <div class="shell page">
    <header class="page-heading">
      <div>
        <span class="section-label">{{ kicker }}</span>
        <h1>{{ title }}</h1>
        <p v-if="total">{{ formatCount(total) }} entities · showing {{ offset + 1 }}–{{ rangeEnd }}</p>
      </div>
    </header>

    <div class="browse-controls">
      <label v-if="!lockKind" class="browse-control">
        <span class="sr-only">Domain</span>
        <select :value="effectiveKind" @change="patchQuery({ kind: ($event.target as HTMLSelectElement).value || undefined })">
          <option value="">Every domain</option>
          <option v-for="item in FILTER_KINDS" :key="item.kind" :value="item.kind">{{ item.plural }}</option>
        </select>
      </label>

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
  </div>
</template>

<style scoped>
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
}
</style>
