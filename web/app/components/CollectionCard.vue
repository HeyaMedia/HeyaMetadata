<script setup lang="ts">
import type { CollectionCard } from '~/utils/types'

// Compact franchise tile: a backdrop with the title, plus a filmstrip peek of
// member posters so it reads as a *collection* at a glance. Used both on the
// homepage rail and the collections directory. Never a half-viewport 2:3
// poster placeholder.
const props = defineProps<{ collection: CollectionCard }>()

const to = computed(() => `/collections/${encodeURIComponent(props.collection.id)}`)
const members = computed(() => [...(props.collection.members ?? [])].sort((a, b) => (a.order ?? 0) - (b.order ?? 0)))
const memberCount = computed(() => members.value.length)
// Up to five posters for the strip; a franchise with none still gets a clean card.
const strip = computed(() => members.value.filter(m => m.image_id).slice(0, 5))
const yearRange = computed(() => {
  const years = members.value.map(m => m.year).filter((y): y is number => typeof y === 'number' && y > 0)
  if (!years.length) return ''
  const lo = Math.min(...years); const hi = Math.max(...years)
  return lo === hi ? String(lo) : `${lo}–${hi}`
})
</script>

<template>
  <NuxtLink :to="to" class="collection-card">
    <span class="collection-card__art">
      <MetadataImage :image-id="collection.image_id" :alt="collection.name" variant="card" decorative />
      <span class="collection-card__scrim" />
      <span class="collection-card__body">
        <small class="collection-card__kind">Franchise</small>
        <strong class="collection-card__title">{{ collection.name }}</strong>
        <span class="collection-card__meta">
          {{ memberCount }} {{ memberCount === 1 ? 'film' : 'films' }}<template v-if="yearRange"> · {{ yearRange }}</template>
        </span>
      </span>
    </span>
    <span v-if="strip.length" class="collection-card__strip" aria-hidden="true">
      <span v-for="(member, index) in strip" :key="member.entity_id || index" class="collection-card__poster">
        <MetadataImage :image-id="member.image_id" variant="thumb" decorative />
      </span>
    </span>
  </NuxtLink>
</template>

<style scoped>
.collection-card {
  position: relative;
  display: flex;
  flex-direction: column;
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
  background: linear-gradient(to top, rgba(8, 11, 13, 0.94) 0%, rgba(8, 11, 13, 0.2) 58%, transparent 100%);
}
.collection-card__body {
  position: absolute;
  inset-inline: 0;
  bottom: 0;
  display: flex;
  flex-direction: column;
  padding: 0.8rem 0.9rem;
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

.collection-card__strip {
  display: flex;
  gap: 2px;
  padding: 2px;
  background: var(--panel-2);
  border-top: 1px solid var(--line-soft);
}
.collection-card__poster {
  flex: 1 1 0;
  min-width: 0;
  aspect-ratio: 2 / 3;
  overflow: hidden;
  border-radius: 2px;
}
</style>
