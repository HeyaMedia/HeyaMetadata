<script setup lang="ts">
import type { PersonCredit } from '~/utils/types'

// Filmography, grouped into acting and crew, each shown as a MediaShelf (rail +
// pan + expand). Only entries with a canonical entity_id are clickable (via
// entityPath); unresolved provider entries stay visible but non-interactive.
// Canonical credits sort first so the resolved titles aren't buried under
// hundreds of unresolved provider entries.
const props = defineProps<{ credits?: PersonCredit[] }>()

// A credit is navigable only when the backend materialized its target entity;
// unresolved items are display-only (never a route from a provider id).
function linkFor(credit: PersonCredit): string | undefined {
  if (!credit.entity_id || credit.resolution_state === 'unresolved') return undefined
  return entityPath({ id: credit.entity_id, kind: credit.kind })
}

const groups = computed(() => {
  const credits = props.credits ?? []
  const cast = credits.filter(c => (c.credit_type ?? 'cast') === 'cast')
  const crew = credits.filter(c => (c.credit_type ?? 'cast') !== 'cast')
  const rank = (c: PersonCredit) => (linkFor(c) ? 0 : 1)
  const order = (a: PersonCredit, b: PersonCredit) => rank(a) - rank(b) || (b.year ?? 0) - (a.year ?? 0)
  return [
    { key: 'cast', title: 'Acting', items: [...cast].sort(order) },
    { key: 'crew', title: 'Crew', items: [...crew].sort(order) },
  ].filter(group => group.items.length)
})

function roleOf(credit: PersonCredit) {
  return formatValue(credit.character) || titleCase(credit.job) || titleCase(credit.department)
}
function creditKey(credit: PersonCredit, index: number) {
  return credit.entity_id || index
}
const linkTag = resolveComponent('NuxtLink')
</script>

<template>
  <MediaShelf
    v-for="group in groups"
    :key="group.key"
    :title="group.title"
    kicker="Filmography"
    :items="group.items"
    shape="poster"
    :item-key="creditKey"
  >
    <template #default="{ item: credit }">
      <component
        :is="linkFor(credit) ? linkTag : 'div'"
        :to="linkFor(credit)"
        class="film"
        :class="{ 'is-ghost': !linkFor(credit) }"
      >
        <span class="film__art"><MetadataImage :image-id="credit.image_id" :alt="credit.title" variant="card" /></span>
        <span class="film__body">
          <small>{{ credit.year || '—' }}</small>
          <strong>{{ formatValue(credit.title) || 'Untitled' }}</strong>
          <span v-if="roleOf(credit)" class="film__role">{{ roleOf(credit) }}</span>
          <span v-if="!linkFor(credit)" class="film__status">Not materialized</span>
        </span>
      </component>
    </template>
  </MediaShelf>
</template>

<style scoped>
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
