<script setup lang="ts">
import type { Credit } from '~/utils/types'

// Full cast & crew, fetched from /entities/{id}/credits. People are deduped
// across providers (the same actor arrives from tmdb + tvdb), split into cast
// and crew, and the cast wall expands from a preview to the full list.
//
// NOTE (backend gap): credits only carry provider_person_id, so cards are not
// yet links. A `/persons/{provider}/{id}/credits` reverse index would unlock
// real actor pages — see the frontend brief's working agreement.
const props = defineProps<{ entityId: string }>()

const api = useHeyaApi()
const { data, pending } = useAsyncData(
  `credits:${props.entityId}`,
  () => api.entityCredits(props.entityId).then(r => r.results ?? []).catch(() => [] as Credit[]),
  { watch: [() => props.entityId], default: () => [] as Credit[] },
)

interface Person { name: string; role: string; imageId?: string; order: number }

function dedupe(credits: Credit[], type: 'cast' | 'crew'): Person[] {
  const byKey = new Map<string, Person>()
  for (const credit of credits) {
    const isCast = (credit.credit_type ?? 'cast') === 'cast'
    if (type === 'cast' ? !isCast : isCast) continue
    const name = formatValue(credit.display_name)
    if (!name) continue
    const role = isCast
      ? formatValue(credit.character)
      : titleCase(credit.job ?? credit.credit_type)
    // Cast: one card per actor (providers give slightly different character
    // strings, so key on name alone). Crew: keep per-role (a person can hold
    // several jobs, e.g. Writer + Director).
    const key = isCast ? name.toLowerCase() : `${name.toLowerCase()}::${role.toLowerCase()}`
    const existing = byKey.get(key)
    const order = credit.order ?? 999
    if (!existing) {
      byKey.set(key, { name, role, imageId: credit.profile_image_id, order })
    } else {
      if (!existing.imageId && credit.profile_image_id) existing.imageId = credit.profile_image_id
      if (order < existing.order) { existing.order = order; if (isCast && role) existing.role = role }
    }
  }
  return [...byKey.values()].sort((a, b) => a.order - b.order)
}

const cast = computed(() => dedupe(data.value, 'cast'))
const crew = computed(() => dedupe(data.value, 'crew'))

const PREVIEW = 12
const showAllCast = ref(false)
const shownCast = computed(() => (showAllCast.value ? cast.value : cast.value.slice(0, PREVIEW)))
</script>

<template>
  <section v-if="pending || cast.length || crew.length" class="cast">
    <header class="section-head">
      <div><span class="section-label">People</span><h2>Cast &amp; crew</h2></div>
      <button v-if="cast.length > PREVIEW" type="button" class="btn--link" @click="showAllCast = !showAllCast">
        {{ showAllCast ? 'Show less' : `Show all ${cast.length}` }}
      </button>
    </header>

    <div v-if="pending" class="media-grid is-portrait">
      <div v-for="n in 8" :key="n" class="skeleton cast__skel" />
    </div>

    <template v-else>
      <div v-if="cast.length" class="media-grid is-portrait">
        <PersonCard v-for="(person, index) in shownCast" :key="`cast-${index}`" :name="person.name" :role="person.role" :image-id="person.imageId" />
      </div>

      <div v-if="crew.length" class="cast__crew">
        <h3 class="cast__crew-title">Crew</h3>
        <ul class="crew-list">
          <li v-for="(person, index) in crew" :key="`crew-${index}`">
            <strong>{{ person.name }}</strong>
            <span v-if="person.role">{{ person.role }}</span>
          </li>
        </ul>
      </div>
    </template>
  </section>
</template>

<style scoped>
.cast { margin-top: 2.5rem; }
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
.cast__skel { aspect-ratio: 3 / 4; border-radius: var(--radius); }
.cast__crew { margin-top: 2rem; }
.cast__crew-title { margin: 0 0 1rem; font-size: 0.85rem; font-weight: 500; }
.crew-list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 0.5rem 1.5rem;
  margin: 0;
  padding: 0;
  list-style: none;
}
.crew-list li {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.5rem 0;
  border-top: 1px solid var(--line-soft);
  font-size: 0.76rem;
}
.crew-list strong { font-weight: 500; }
.crew-list span { color: var(--muted-2); text-align: right; }
</style>
