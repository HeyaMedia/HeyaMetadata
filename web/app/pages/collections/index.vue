<script setup lang="ts">
const api = useHeyaApi()
const { data, pending } = await useAsyncData(
  'collections',
  () => api.collections().then(r => r.collections ?? []),
  { default: () => [] },
)
</script>

<template>
  <div class="shell page">
    <header class="page-heading">
      <div>
        <span class="section-label">Provider-backed franchises</span>
        <h1>Collections</h1>
        <p v-if="data?.length">{{ data.length }} movie collections encountered in canonical records.</p>
      </div>
    </header>

    <LoadingSkeleton v-if="pending" layout="grid" shape="landscape" :count="8" />

    <div v-else-if="data?.length" class="media-grid is-landscape">
      <CollectionCard v-for="item in data" :key="item.provider_id" :collection="item" />
    </div>

    <EmptyState
      v-else
      title="No collections yet."
      message="They appear as soon as an ingested movie belongs to a provider-backed franchise."
    />
  </div>
</template>
