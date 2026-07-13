<script setup lang="ts">
import type { Relation } from '~/utils/types'

// Generic linked list over a relation_type (e.g. an album's `editions` →
// release pages). Links to the canonical target when target_entity_id exists.
const props = withDefaults(defineProps<{
  entityId: string
  type: string
  title: string
  kicker?: string
}>(), { kicker: 'Related' })

const api = useHeyaApi()
const { data } = useAsyncData(
  `relations:${props.type}:${props.entityId}`,
  () => api.allEntityRelations(props.entityId, props.type).then(r => r.relations ?? []).catch(() => [] as Relation[]),
  { watch: [() => props.entityId, () => props.type], default: () => [] as Relation[] },
)

interface Entry { title: string; sub: string; date: string; to?: string }

const entries = computed<Entry[]>(() => {
  const seen = new Set<string>()
  const out: Entry[] = []
  for (const relation of data.value) {
    if (relation.relation_type !== props.type) continue
    const meta = relation.metadata ?? {}
    const title = formatValue(meta.title) || titleCase(relation.target_kind) || 'Related'
    const key = relation.target_entity_id || relation.provider_value || title
    if (seen.has(key)) continue
    seen.add(key)
    // date can be a plain string or a { type, value, precision } object.
    const date = formatValue(meta.date?.value ?? (typeof meta.date === 'string' ? meta.date : ''))
    out.push({
      title,
      sub: [formatValue(meta.country), titleCase(meta.status), formatValue(meta.barcode)].filter(Boolean).join(' · '),
      date,
      to: relation.target_entity_id ? entityPath({ id: relation.target_entity_id, kind: relation.target_kind }) : undefined,
    })
  }
  return out
})
const linkTag = resolveComponent('NuxtLink')
</script>

<template>
  <OverviewPanel v-if="entries.length" :title="`${title} (${entries.length})`" :kicker="kicker" full>
    <ol class="line-list">
      <li v-for="(entry, index) in entries" :key="index">
        <span class="line-list__main">
          <component :is="entry.to ? linkTag : 'span'" :to="entry.to" class="rel__title" :class="{ 'is-link': entry.to }">
            {{ entry.title }}<template v-if="entry.to"> ↗</template>
          </component>
          <span v-if="entry.sub" class="line-list__sub">{{ entry.sub }}</span>
        </span>
        <span v-if="entry.date" class="line-list__meta">{{ formatDate(entry.date) }}</span>
      </li>
    </ol>
  </OverviewPanel>
</template>

<style scoped>
.rel__title.is-link:hover { color: var(--gold); }
</style>
