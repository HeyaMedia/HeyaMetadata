<script setup lang="ts">
import type { CollectionCard } from '~/utils/types'

// Compact landscape/backdrop treatment for a movie franchise. Never a
// half-viewport 2:3 poster placeholder.
const props = defineProps<{ collection: CollectionCard }>()

const memberCount = computed(() => props.collection.members?.length ?? 0)
const to = computed(() => `/collections/${encodeURIComponent(props.collection.provider_id)}`)
</script>

<template>
  <NuxtLink :to="to" class="collection-card">
    <span class="collection-card__art">
      <MetadataImage :image-id="collection.image_id" :alt="collection.name" variant="card" decorative />
      <span class="collection-card__scrim" />
    </span>
    <span class="collection-card__body">
      <small class="collection-card__kind">Franchise</small>
      <strong class="collection-card__title">{{ collection.name }}</strong>
      <span class="collection-card__meta">{{ memberCount }} {{ memberCount === 1 ? 'film' : 'films' }}</span>
    </span>
  </NuxtLink>
</template>

<style scoped>
.collection-card {
  position: relative;
  display: block;
  overflow: hidden;
  border: 1px solid var(--line-soft);
  border-radius: var(--radius);
  background: var(--panel);
  transition: transform 0.18s ease, border-color 0.18s ease;
}
.collection-card:hover { transform: translateY(-3px); border-color: #5a5236; }
.collection-card__art { position: relative; display: block; aspect-ratio: 16 / 9; overflow: hidden; }
.collection-card__scrim {
  position: absolute;
  inset: 0;
  background: linear-gradient(to top, rgba(8, 11, 13, 0.92) 0%, rgba(8, 11, 13, 0.15) 55%, transparent 100%);
}
.collection-card__body {
  position: absolute;
  inset-inline: 0;
  bottom: 0;
  display: flex;
  flex-direction: column;
  padding: 0.85rem 0.9rem;
}
.collection-card__kind {
  color: var(--gold);
  font-family: var(--font-mono);
  font-size: 0.56rem;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.collection-card__title {
  margin-top: 0.25rem;
  overflow: hidden;
  font-size: 0.95rem;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.collection-card__meta { margin-top: 0.15rem; color: #b8c0bd; font-size: 0.68rem; }
</style>
