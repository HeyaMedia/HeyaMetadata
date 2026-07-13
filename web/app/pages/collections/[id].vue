<script setup lang="ts">
// Collection detail. Provider-backed, addressed by TMDB collection id. Members
// with an entity_id link to the canonical movie; members without stay visibly
// "not ingested" and never trigger silent upstream ingestion on click.
const route = useRoute()
const api = useHeyaApi()
const id = computed(() => route.params.id as string)

const { data: collection, pending, error } = await useAsyncData(
  'collection',
  () => api.collection(id.value),
  { watch: [id] },
)

const members = computed(() =>
  [...(collection.value?.members ?? [])].sort((a, b) => (a.order ?? 0) - (b.order ?? 0)),
)
const linkTag = resolveComponent('NuxtLink')
</script>

<template>
  <div class="shell detail-page">
    <NuxtLink to="/collections" class="back-link">← All collections</NuxtLink>

    <EmptyState v-if="!collection && !pending" title="Collection unavailable." :message="error || 'This collection could not be loaded.'" />

    <template v-else-if="collection">
      <header class="collection-hero">
        <div class="collection-hero__art">
          <MetadataImage :image-id="collection.image_id" :alt="collection.name" variant="hero" decorative />
        </div>
        <div class="collection-hero__body">
          <span class="section-label">{{ formatKey(collection.provider || 'tmdb') }} collection · {{ members.length }} films</span>
          <h1 class="editorial">{{ collection.name }}</h1>
          <p v-if="collection.overview" class="collection-hero__overview">{{ collection.overview }}</p>
        </div>
      </header>

      <section class="collection-members">
        <h2 class="collection-members__title">Members <small>release order</small></h2>
        <div class="media-grid is-poster">
          <component
            :is="member.entity_id ? linkTag : 'div'"
            v-for="member in members"
            :key="member.provider_id"
            :to="member.entity_id ? `/movies/${member.entity_id}` : undefined"
            class="member"
            :class="{ 'member--ghost': !member.entity_id }"
          >
            <span class="member__art">
              <MetadataImage :image-id="member.image_id" :alt="member.title" variant="card" />
            </span>
            <span class="member__body">
              <small>{{ member.year || 'TBA' }}</small>
              <strong>{{ member.title }}</strong>
              <span class="member__status" :class="{ 'is-ingested': member.entity_id }">
                {{ member.entity_id ? 'Canonical entity ↗' : 'Not ingested' }}
              </span>
            </span>
          </component>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.collection-hero {
  display: grid;
  grid-template-columns: minmax(14rem, 22rem) 1fr;
  gap: clamp(1.75rem, 4vw, 3.5rem);
  align-items: center;
  padding-top: 1rem;
}
.collection-hero__art { aspect-ratio: 16 / 9; overflow: hidden; border: 1px solid var(--line); border-radius: var(--radius); }
.collection-hero__body h1 { margin: 0.6rem 0 1rem; font-size: clamp(2rem, 4vw, 3.4rem); }
.collection-hero__overview { max-width: 46rem; margin: 0; color: #97a19f; font-size: 0.85rem; line-height: 1.75; }

.collection-members { margin-top: 3rem; }
.collection-members__title {
  display: flex;
  align-items: baseline;
  gap: 0.6rem;
  margin: 0 0 1.25rem;
  padding-bottom: 0.75rem;
  border-bottom: 1px solid var(--line);
  font-size: 1.15rem;
  font-weight: 500;
}
.collection-members__title small { color: var(--muted-2); font-size: 0.66rem; text-transform: uppercase; letter-spacing: 0.1em; }

.member {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--line-soft);
  border-radius: var(--radius);
  background: var(--panel);
  transition: transform 0.18s ease, border-color 0.18s ease;
}
.member:not(.member--ghost):hover { transform: translateY(-3px); border-color: #5a5236; }
.member--ghost { opacity: 0.62; }
.member__art { aspect-ratio: 2 / 3; overflow: hidden; }
.member__body { display: flex; flex-direction: column; padding: 0.7rem 0.75rem 0.85rem; }
.member__body small { color: var(--gold); font-family: var(--font-mono); font-size: 0.58rem; }
.member__body strong { margin-top: 0.3rem; overflow: hidden; font-size: 0.82rem; text-overflow: ellipsis; white-space: nowrap; }
.member__status { margin-top: 0.2rem; color: var(--muted-2); font-size: 0.64rem; }
.member__status.is-ingested { color: var(--green); }

@media (max-width: 720px) {
  .collection-hero { grid-template-columns: 1fr; }
}
</style>
