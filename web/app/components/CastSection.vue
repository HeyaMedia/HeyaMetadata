<script setup lang="ts">
import type { Credit } from '~/utils/types'

// Cast & crew from /entities/{id}/credits. Cast renders as a MediaShelf (rail +
// pan + expand); crew is a compact list capped with a "show all". People are
// deduped across providers (cast by actor, crew by role) and link to their
// canonical person page via person_entity_id when reconciled.
const props = defineProps<{ entityId: string }>()

const api = useHeyaApi()
const { data, pending } = useAsyncData(
  `credits:${props.entityId}`,
  () => api.entityCredits(props.entityId).then(r => r.results ?? []).catch(() => [] as Credit[]),
  { watch: [() => props.entityId], default: () => [] as Credit[] },
)

interface Person { name: string; role: string; imageId?: string; order: number; to?: string }

function personLink(credit: Credit): string | undefined {
  if (credit.person_entity_id) return entityPath({ id: credit.person_entity_id, kind: 'person' })
  if (credit.provider && credit.provider_person_id) {
    return `/people/${encodeURIComponent(credit.provider)}/${encodeURIComponent(credit.provider_person_id)}`
  }
  return undefined
}

function dedupe(credits: Credit[], type: 'cast' | 'crew'): Person[] {
  const byKey = new Map<string, Person>()
  for (const credit of credits) {
    const isCast = (credit.credit_type ?? 'cast') === 'cast'
    if (type === 'cast' ? !isCast : isCast) continue
    const name = formatValue(credit.display_name)
    if (!name) continue
    const role = isCast ? formatValue(credit.character) : titleCase(credit.job ?? credit.credit_type)
    const key = isCast ? name.toLowerCase() : `${name.toLowerCase()}::${role.toLowerCase()}`
    const existing = byKey.get(key)
    const order = credit.order ?? 999
    const to = personLink(credit)
    const isTmdb = credit.provider === 'tmdb'
    if (!existing) {
      byKey.set(key, { name, role, imageId: credit.profile_image_id, order, to })
    } else {
      if (!existing.imageId && credit.profile_image_id) existing.imageId = credit.profile_image_id
      if (to && (!existing.to || isTmdb)) existing.to = to
      if (order < existing.order) { existing.order = order; if (isCast && role) existing.role = role }
    }
  }
  return [...byKey.values()].sort((a, b) => a.order - b.order)
}

const cast = computed(() => dedupe(data.value, 'cast'))
const crew = computed(() => dedupe(data.value, 'crew'))

const CREW_PREVIEW = 18
const showAllCrew = ref(false)
const shownCrew = computed(() => (showAllCrew.value ? crew.value : crew.value.slice(0, CREW_PREVIEW)))
</script>

<template>
  <div v-if="pending || cast.length || crew.length">
    <p v-if="pending" class="muted cast__loading">Loading cast…</p>

    <MediaShelf v-if="cast.length" title="Cast" kicker="People" :items="cast" shape="portrait" :item-key="(_, i) => i">
      <template #default="{ item: person }">
        <PersonCard :name="person.name" :role="person.role" :image-id="person.imageId" :to="person.to" />
      </template>
    </MediaShelf>

    <section v-if="crew.length" class="detail-section cast__crew">
      <header class="section-head">
        <div><span class="section-label">People</span><h2>Crew <small>{{ crew.length }}</small></h2></div>
        <button v-if="crew.length > CREW_PREVIEW" type="button" class="btn--link" @click="showAllCrew = !showAllCrew">
          {{ showAllCrew ? 'Show less' : `Show all ${crew.length}` }}
        </button>
      </header>
      <ul class="crew-list">
        <li v-for="(person, index) in shownCrew" :key="index">
          <strong>{{ person.name }}</strong>
          <span v-if="person.role">{{ person.role }}</span>
        </li>
      </ul>
    </section>
  </div>
</template>

<style scoped>
.cast__loading { margin: 2rem 0 0; font-size: 0.8rem; }
.crew-list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 0 1.5rem;
  margin: 0;
  padding: 0;
  list-style: none;
}
.crew-list li {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.4rem 0;
  border-top: 1px solid var(--line-soft);
  font-size: 0.75rem;
}
.crew-list strong { font-weight: 500; }
.crew-list span { color: var(--muted-2); text-align: right; }
</style>
