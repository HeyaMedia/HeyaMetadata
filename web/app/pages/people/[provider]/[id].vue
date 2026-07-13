<script setup lang="ts">
// Provider-scoped filmography — the compatibility resolver for old links and
// unresolved state. Redirects to the canonical /people/{id} once the person has
// a reconciled entity id; otherwise renders the provider-scoped view.
const route = useRoute()
const api = useHeyaApi()
const provider = computed(() => route.params.provider as string)
const personId = computed(() => route.params.id as string)

const { data, pending, error } = await useAsyncData(
  'person-provider',
  () => api.personCredits(provider.value, personId.value),
  { watch: [provider, personId] },
)

// Prefer the canonical person page when the backend has reconciled an entity id.
watch(data, value => {
  if (value?.person?.entity_id) navigateTo(entityPath({ id: value.person.entity_id, kind: 'person' }), { replace: true })
}, { immediate: true })

const person = computed(() => data.value?.person)
const credits = computed(() => data.value?.credits ?? [])
</script>

<template>
  <div class="shell detail-page">
    <NuxtLink to="/browse" class="back-link">← Browse library</NuxtLink>

    <EmptyState v-if="!data && !pending" title="Person unavailable." :message="error || 'No credits found for this provider person.'" />

    <template v-else-if="person">
      <header class="person-hero">
        <div class="person-hero__art">
          <MetadataImage :image-id="person.profile_image_id" :alt="person.display_name" variant="hero" />
        </div>
        <div class="person-hero__body">
          <p class="hero__kicker"><span>Person</span><i aria-hidden="true" />{{ formatKey(person.provider || '') }}</p>
          <h1 class="editorial person-hero__name">{{ person.display_name }}</h1>
          <p class="person-hero__count">{{ credits.length }} credited {{ credits.length === 1 ? 'title' : 'titles' }} · provider view</p>
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
.hero__kicker { display: flex; align-items: center; gap: 0.6rem; margin: 0; color: #8b9697; font-family: var(--font-mono); font-size: 0.64rem; letter-spacing: 0.1em; text-transform: uppercase; }
.hero__kicker span { color: var(--gold); }
.hero__kicker i { width: 0.25rem; height: 0.25rem; border-radius: 50%; background: #536064; }

@media (max-width: 640px) { .person-hero { grid-template-columns: 1fr; } .person-hero__art { width: min(11rem, 50vw); } }
</style>
