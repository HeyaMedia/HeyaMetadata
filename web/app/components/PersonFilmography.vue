<script setup lang="ts">
import type { PersonCredit } from '~/utils/types'

// Filmography grid shared by the canonical person page and the provider-scoped
// resolver. Groups by cast/crew, sorts by year, links each title via entityPath.
const props = defineProps<{ credits?: PersonCredit[] }>()

const groups = computed(() => {
  const credits = props.credits ?? []
  const cast = credits.filter(c => (c.credit_type ?? 'cast') === 'cast')
  const crew = credits.filter(c => (c.credit_type ?? 'cast') !== 'cast')
  const byYear = (a: PersonCredit, b: PersonCredit) => (b.year ?? 0) - (a.year ?? 0)
  return [
    { key: 'cast', title: 'Acting', items: [...cast].sort(byYear) },
    { key: 'crew', title: 'Crew', items: [...crew].sort(byYear) },
  ].filter(group => group.items.length)
})

function synth(credit: PersonCredit) {
  return { id: credit.entity_id, kind: credit.kind, display: { title: credit.title, year: credit.year, image_id: credit.image_id } }
}
</script>

<template>
  <section v-for="group in groups" :key="group.key" class="filmography">
    <header class="section-head">
      <div><span class="section-label">Filmography</span><h2>{{ group.title }} <small>{{ group.items.length }}</small></h2></div>
    </header>
    <div class="media-grid is-poster">
      <NuxtLink v-for="credit in group.items" :key="`${credit.entity_id}-${credit.character || credit.job || ''}`" :to="entityPath(synth(credit))" class="film">
        <span class="film__art"><MetadataImage :image-id="credit.image_id" :alt="credit.title" variant="card" /></span>
        <span class="film__body">
          <small>{{ credit.year || '—' }}</small>
          <strong>{{ formatValue(credit.title) || 'Untitled' }}</strong>
          <span v-if="credit.character || credit.job" class="film__role">{{ formatValue(credit.character) || titleCase(credit.job) }}</span>
        </span>
      </NuxtLink>
    </div>
  </section>
</template>

<style scoped>
.filmography { margin-top: 2.5rem; }
.section-head { margin-bottom: 1.25rem; padding-bottom: 0.75rem; border-bottom: 1px solid var(--line); }
.section-head h2 { display: flex; align-items: baseline; gap: 0.5rem; margin: 0.3rem 0 0; font-size: 1.2rem; font-weight: 500; }
.section-head small { color: var(--muted-2); font-family: var(--font-mono); font-size: 0.68rem; }
.film {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--line-soft);
  border-radius: var(--radius);
  background: var(--panel);
  transition: transform 0.18s ease, border-color 0.18s ease;
}
.film:hover { transform: translateY(-3px); border-color: #5a5236; }
.film__art { aspect-ratio: 2 / 3; overflow: hidden; }
.film__body { display: flex; flex-direction: column; padding: 0.6rem 0.7rem 0.75rem; }
.film__body small { color: var(--gold); font-family: var(--font-mono); font-size: 0.56rem; }
.film__body strong { margin-top: 0.3rem; overflow: hidden; font-size: 0.8rem; text-overflow: ellipsis; white-space: nowrap; }
.film__role { margin-top: 0.2rem; overflow: hidden; color: var(--muted-2); font-size: 0.66rem; text-overflow: ellipsis; white-space: nowrap; }
</style>
