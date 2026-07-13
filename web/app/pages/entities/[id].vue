<script setup lang="ts">
// Fallback for unrecognized / future kinds. Known kinds are redirected to their
// canonical route; everything else renders through GenericOverview.
const route = useRoute()
const id = computed(() => route.params.id as string)
</script>

<template>
  <EntityDetail :id="id" redirect-to-canonical />
</template>
