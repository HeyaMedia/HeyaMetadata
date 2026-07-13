<script setup lang="ts">
const route = useRoute()
const id = computed(() => route.params.id as string)
</script>

<template>
  <EntityDetail :id="id" expected-kind="artist">
    <template #default="{ entity }">
      <ArtistOverview :entity="entity" />
    </template>
  </EntityDetail>
</template>
