<script setup lang="ts">
import type { CollectionMember } from '~/utils/types'

// Rail of related titles (e.g. the other films in a movie's collection), as a
// MediaShelf. When a collectionId is given we fetch /collections/{id} to hydrate
// entity_id — enabling links and excluding the current film. Members without an
// entity_id stay visible but non-interactive ("not ingested").
const props = withDefaults(defineProps<{
  members?: CollectionMember[]
  collectionId?: string
  excludeId?: string
  title: string
  kicker?: string
  kind?: string
}>(), { members: () => [], collectionId: '', excludeId: '', kicker: 'Franchise', kind: 'movie' })

const api = useHeyaApi()
const { data: fetched } = useAsyncData(
  `collection-members:${props.collectionId || 'none'}`,
  () => (props.collectionId ? api.collection(props.collectionId).then(c => c.members ?? []).catch(() => []) : Promise.resolve([] as CollectionMember[])),
  { watch: [() => props.collectionId], default: () => [] as CollectionMember[] },
)

const source = computed(() => (props.collectionId ? (fetched.value ?? []) : (props.members ?? [])))
const items = computed(() =>
  [...source.value]
    .filter(member => member.entity_id !== props.excludeId)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0)),
)
const linkTag = resolveComponent('NuxtLink')
</script>

<template>
  <MediaShelf :title="title" :kicker="kicker" :items="items" shape="poster" :item-key="m => m.provider_id">
    <template #default="{ item: member }">
      <component
        :is="member.entity_id ? linkTag : 'div'"
        :to="member.entity_id ? entityPath({ id: member.entity_id, kind }) : undefined"
        class="member-card"
        :class="{ 'is-ghost': !member.entity_id }"
      >
        <span class="member-card__art"><MetadataImage :image-id="member.image_id" :alt="member.title" variant="card" /></span>
        <span class="member-card__body">
          <small>{{ member.year || 'TBA' }}</small>
          <strong>{{ member.title }}</strong>
          <span class="member-card__status" :class="{ 'is-ingested': member.entity_id }">
            {{ member.entity_id ? 'Canonical ↗' : 'Not ingested' }}
          </span>
        </span>
      </component>
    </template>
  </MediaShelf>
</template>

<style scoped>
.member-card {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--line-soft);
  border-radius: var(--radius);
  background: var(--panel);
  transition: transform 0.18s ease, border-color 0.18s ease;
}
.member-card:not(.is-ghost):hover { transform: translateY(-3px); border-color: #5a5236; }
.member-card.is-ghost { opacity: 0.6; }
.member-card__art { aspect-ratio: 2 / 3; overflow: hidden; }
.member-card__body { display: flex; flex-direction: column; padding: 0.55rem 0.65rem 0.7rem; }
.member-card__body small { color: var(--gold); font-family: var(--font-mono); font-size: 0.56rem; }
.member-card__body strong { margin-top: 0.28rem; overflow: hidden; font-size: 0.8rem; text-overflow: ellipsis; white-space: nowrap; }
.member-card__status { margin-top: 0.2rem; color: var(--muted-2); font-size: 0.62rem; }
.member-card__status.is-ingested { color: var(--green); }
</style>
