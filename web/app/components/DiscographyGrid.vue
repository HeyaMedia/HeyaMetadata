<script setup lang="ts">
import type { Relation } from '~/utils/types'

// Artist discography from /entities/{id}/relations (relation_type=discography).
// Items link to the canonical album page when target_entity_id exists; others
// are shown but marked "not ingested" (no silent resolution on click). Relations
// carry no artwork, so this is a dense grouped list rather than a poster grid.
const props = defineProps<{ entityId: string }>()

const api = useHeyaApi()
const { data, pending } = useAsyncData(
  `discography:${props.entityId}`,
  () => api.entityRelations(props.entityId, { type: 'discography', limit: 100 }).then(r => r.relations ?? []).catch(() => [] as Relation[]),
  { watch: [() => props.entityId], default: () => [] as Relation[] },
)

interface Entry { title: string; kind: string; date: string; year: string; to?: string }

const entries = computed<Entry[]>(() => {
  const seen = new Set<string>()
  const out: Entry[] = []
  for (const relation of data.value) {
    if (relation.relation_type !== 'discography') continue
    const source = relation.metadata?.sources?.[0] ?? {}
    const title = formatValue(relation.metadata?.title ?? source.title)
    if (!title) continue
    const to = relation.target_entity_id ? entityPath({ id: relation.target_entity_id, kind: relation.target_kind }) : undefined
    const key = relation.target_entity_id || `${title}:${source.date ?? ''}`
    if (seen.has(key)) continue
    seen.add(key)
    const date = formatValue(source.date)
    out.push({ title, kind: titleCase(source.kind) || 'Release', date, year: date.slice(0, 4), to })
  }
  return out.sort((a, b) => (b.date || '').localeCompare(a.date || ''))
})

const groups = computed(() => {
  const order = ['Album', 'Ep', 'Single', 'Compilation', 'Live', 'Release']
  const map = new Map<string, Entry[]>()
  for (const entry of entries.value) {
    const bucket = entry.kind || 'Release'
    if (!map.has(bucket)) map.set(bucket, [])
    map.get(bucket)!.push(entry)
  }
  return [...map.entries()]
    .sort((a, b) => (order.indexOf(a[0]) + 1 || 99) - (order.indexOf(b[0]) + 1 || 99))
    .map(([kind, items]) => ({ kind, items }))
})
const linkTag = resolveComponent('NuxtLink')
</script>

<template>
  <section v-if="pending || entries.length" class="disco">
    <header class="section-head">
      <div><span class="section-label">Catalogue</span><h2>Discography</h2></div>
      <span v-if="entries.length" class="disco__count">{{ entries.length }} releases</span>
    </header>

    <p v-if="pending" class="muted">Loading discography…</p>

    <div v-else class="disco__groups">
      <div v-for="group in groups" :key="group.kind" class="disco__group">
        <h3 class="disco__group-title">{{ group.kind === 'Ep' ? 'EPs' : `${group.kind}s` }} <small>{{ group.items.length }}</small></h3>
        <ul class="line-list">
          <li v-for="(entry, index) in group.items" :key="index">
            <span class="line-list__main">
              <component :is="entry.to ? linkTag : 'span'" :to="entry.to" class="disco__link" :class="{ 'is-ghost': !entry.to }">
                {{ entry.title }}<template v-if="entry.to"> ↗</template>
              </component>
              <span v-if="!entry.to" class="disco__ghost-note">not ingested</span>
            </span>
            <span v-if="entry.year" class="line-list__meta">{{ entry.year }}</span>
          </li>
        </ul>
      </div>
    </div>
  </section>
</template>

<style scoped>
.disco { margin-top: 2.5rem; }
.section-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1.25rem;
  padding-bottom: 0.75rem;
  border-bottom: 1px solid var(--line);
}
.section-head h2 { margin: 0.3rem 0 0; font-size: 1.2rem; font-weight: 500; }
.disco__count { color: var(--muted-2); font-family: var(--font-mono); font-size: 0.68rem; }
.disco__groups { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 1.5rem 2.5rem; align-items: start; }
.disco__group-title { display: flex; align-items: baseline; gap: 0.5rem; margin: 0 0 0.5rem; font-size: 0.82rem; font-weight: 500; }
.disco__group-title small { color: var(--muted-2); font-family: var(--font-mono); font-size: 0.66rem; }
.disco__link { color: var(--text-dim); }
.disco__link:not(.is-ghost):hover { color: var(--gold); }
.disco__link.is-ghost { color: var(--muted); }
.disco__ghost-note { display: block; margin-top: 0.1rem; color: var(--muted-2); font-size: 0.6rem; }
</style>
