<script setup lang="ts">
const api = useHeyaApi()
const { data, pending } = await useAsyncData(
  'collections',
  () => api.collections().then(r => r.collections ?? []),
  { default: () => [] },
)

const filmTotal = computed(() =>
  (data.value ?? []).reduce((sum, collection) => sum + (collection.members?.length ?? 0), 0),
)
</script>

<template>
  <div class="shell page">
    <header class="page-heading">
      <div>
        <span class="section-label">Provider-backed franchises</span>
        <h1>Collections</h1>
        <p v-if="data?.length">
          {{ data.length }} {{ data.length === 1 ? 'franchise' : 'franchises' }} spanning {{ filmTotal }} films, assembled
          from the movies already in the canonical library.
        </p>
      </div>
    </header>

    <LoadingSkeleton v-if="pending" layout="grid" shape="landscape" :count="6" />

    <div v-else-if="data?.length" class="collection-grid">
      <CollectionCard v-for="item in data" :key="item.id" :collection="item" />
    </div>

    <EmptyState
      v-else
      title="No collections yet."
      message="They appear as soon as an ingested movie belongs to a provider-backed franchise."
    />
  </div>
</template>

<style scoped>
.collection-grid {
  display: grid;
  gap: 1.1rem;
  grid-template-columns: repeat(auto-fill, minmax(288px, 1fr));
}
</style>
