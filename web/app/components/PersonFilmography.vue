<script setup lang="ts">
import type { PersonCredit } from '~/utils/types'

// Filmography grid shared by the canonical person page and the provider-scoped
// resolver. Groups by cast/crew, sorts by year. Only entries with a canonical
// entity_id are clickable (via entityPath); unresolved provider entries stay
// visible but non-interactive. Large groups collapse to a preview.
const props = defineProps<{ credits?: PersonCredit[] }>()

const PREVIEW = 36

const groups = computed(() => {
  const credits = props.credits ?? []
  const cast = credits.filter(c => (c.credit_type ?? 'cast') === 'cast')
  const crew = credits.filter(c => (c.credit_type ?? 'cast') !== 'cast')
  // Canonical (clickable) credits first, then by year — otherwise the handful of
  // resolved titles get buried under hundreds of unresolved provider entries.
  const rank = (c: PersonCredit) => (c.entity_id ? 0 : 1)
  const order = (a: PersonCredit, b: PersonCredit) => rank(a) - rank(b) || (b.year ?? 0) - (a.year ?? 0)
  return [
    { key: 'cast', title: 'Acting', items: [...cast].sort(order) },
    { key: 'crew', title: 'Crew', items: [...crew].sort(order) },
  ].filter(group => group.items.length)
})

// Per-group expansion state.
const expanded = reactive<Record<string, boolean>>({})
function shown(group: { key: string; items: PersonCredit[] }) {
  return expanded[group.key] ? group.items : group.items.slice(0, PREVIEW)
}

function roleOf(credit: PersonCredit) {
  return formatValue(credit.character) || titleCase(credit.job) || titleCase(credit.department)
}
function synth(credit: PersonCredit) {
  return { id: credit.entity_id, kind: credit.kind, display: { title: credit.title, year: credit.year, image_id: credit.image_id } }
}
const linkTag = resolveComponent('NuxtLink')
</script>

<template>
  <section v-for="group in groups" :key="group.key" class="filmography">
    <header class="section-head">
      <div><span class="section-label">Filmography</span><h2>{{ group.title }} <small>{{ group.items.length }}</small></h2></div>
      <button v-if="group.items.length > PREVIEW" type="button" class="btn--link" @click="expanded[group.key] = !expanded[group.key]">
        {{ expanded[group.key] ? 'Show less' : `Show all ${group.items.length}` }}
      </button>
    </header>
    <div class="media-grid is-poster">
      <component
        :is="credit.entity_id ? linkTag : 'div'"
        v-for="(credit, index) in shown(group)"
        :key="`${credit.entity_id || credit.provider_target_id || index}`"
        :to="credit.entity_id ? entityPath(synth(credit)) : undefined"
        class="film"
        :class="{ 'is-ghost': !credit.entity_id }"
      >
        <span class="film__art"><MetadataImage :image-id="credit.image_id" :alt="credit.title" variant="card" /></span>
        <span class="film__body">
          <small>{{ credit.year || '—' }}</small>
          <strong>{{ formatValue(credit.title) || 'Untitled' }}</strong>
          <span v-if="roleOf(credit)" class="film__role">{{ roleOf(credit) }}</span>
          <span v-if="!credit.entity_id" class="film__status">Not ingested</span>
        </span>
      </component>
    </div>
  </section>
</template>

<style scoped>
.filmography { margin-top: clamp(1.75rem, 3vw, 2.5rem); }
.film {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--line-soft);
  border-radius: var(--radius);
  background: var(--panel);
  transition: transform 0.18s ease, border-color 0.18s ease;
}
.film:not(.is-ghost):hover { transform: translateY(-3px); border-color: #5a5236; }
.film.is-ghost { opacity: 0.62; }
.film__art { aspect-ratio: 2 / 3; overflow: hidden; }
.film__body { display: flex; flex-direction: column; padding: 0.55rem 0.65rem 0.7rem; }
.film__body small { color: var(--gold); font-family: var(--font-mono); font-size: 0.56rem; }
.film__body strong { margin-top: 0.28rem; overflow: hidden; font-size: 0.78rem; text-overflow: ellipsis; white-space: nowrap; }
.film__role { margin-top: 0.18rem; overflow: hidden; color: var(--muted-2); font-size: 0.64rem; text-overflow: ellipsis; white-space: nowrap; }
.film__status { margin-top: 0.2rem; color: var(--muted-2); font-size: 0.6rem; }
</style>
