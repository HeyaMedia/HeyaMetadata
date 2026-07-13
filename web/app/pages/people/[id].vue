<script setup lang="ts">
// Canonical person page (GET /api/v2/persons/{id}) with the combined,
// cross-provider filmography. Cast/crew cards link here via person_entity_id.
const route = useRoute()
const api = useHeyaApi()
const id = computed(() => route.params.id as string)

const { data, pending, error } = await useAsyncData('person-doc', () => api.person(id.value), { watch: [id] })

const name = computed(() => formatValue(data.value?.display?.title) || 'Person')
const image = computed(() => data.value?.display?.image_id)
const credits = computed(() => data.value?.data?.credits ?? [])
const aliases = computed(() => (data.value?.data?.names ?? []).filter(n => formatValue(n) && formatValue(n) !== name.value))
const externalIds = computed(() => data.value?.external_ids ?? [])
</script>

<template>
  <div class="shell detail-page">
    <NuxtLink to="/browse" class="back-link">← Browse library</NuxtLink>

    <EmptyState v-if="!data && !pending" title="Person unavailable." :message="error || 'This person could not be loaded.'" />

    <template v-else-if="data">
      <header class="person-hero">
        <div class="person-hero__art">
          <MetadataImage :image-id="image" :alt="name" variant="hero" />
        </div>
        <div class="person-hero__body">
          <p class="hero__kicker"><span>Person</span></p>
          <h1 class="editorial person-hero__name">{{ name }}</h1>
          <p class="person-hero__count">{{ credits.length }} credited {{ credits.length === 1 ? 'title' : 'titles' }} · combined across providers</p>
          <p v-if="aliases.length" class="person-hero__aliases">Also credited as {{ aliases.join(', ') }}</p>
          <div v-if="externalIds.length" class="chip-row person-hero__ids">
            <span v-for="ext in externalIds" :key="`${ext.provider}:${ext.value}`" class="chip chip--accent">{{ formatKey(ext.provider || '') }}</span>
          </div>
        </div>
      </header>

      <PersonFilmography :credits="credits" />
    </template>
  </div>
</template>

<style scoped>
.person-hero {
  display: grid;
  grid-template-columns: minmax(9rem, 13rem) 1fr;
  gap: clamp(1.5rem, 4vw, 3rem);
  align-items: center;
  padding-top: 1rem;
}
.person-hero__art { aspect-ratio: 3 / 4; overflow: hidden; border: 1px solid var(--line-strong); border-radius: var(--radius); }
.person-hero__name { margin: 0.5rem 0 0.6rem; font-size: clamp(2rem, 4vw, 3.4rem); }
.person-hero__count { margin: 0; color: var(--muted); font-size: 0.82rem; }
.person-hero__aliases { margin: 0.6rem 0 0; color: var(--muted-2); font-size: 0.76rem; }
.person-hero__ids { margin-top: 0.9rem; }
.hero__kicker { display: flex; align-items: center; gap: 0.6rem; margin: 0; color: #8b9697; font-family: var(--font-mono); font-size: 0.64rem; letter-spacing: 0.1em; text-transform: uppercase; }
.hero__kicker span { color: var(--gold); }

@media (max-width: 640px) { .person-hero { grid-template-columns: 1fr; } .person-hero__art { width: min(11rem, 50vw); } }
</style>
