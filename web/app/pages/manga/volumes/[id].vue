<script setup lang="ts">
const route = useRoute()
const id = computed(() => route.params.id as string)
</script>

<template>
  <EntityDetail :id="id" expected-kind="manga_volume" back-to="/manga" back-label="All manga">
    <template #default="{ entity }">
      <VolumeOverview :entity="entity" />
    </template>
  </EntityDetail>
</template>
