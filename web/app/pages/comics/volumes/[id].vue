<script setup lang="ts">
const route = useRoute()
const id = computed(() => route.params.id as string)
</script>

<template>
  <EntityDetail :id="id" expected-kind="comic_volume" back-to="/comics" back-label="All comics">
    <template #default="{ entity }">
      <VolumeOverview :entity="entity" />
    </template>
  </EntityDetail>
</template>
