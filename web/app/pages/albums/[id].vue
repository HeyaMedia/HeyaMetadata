<script setup lang="ts">
const route = useRoute()
const id = computed(() => route.params.id as string)
</script>

<template>
  <EntityDetail :id="id" expected-kind="release_group">
    <template #default="{ entity }">
      <AlbumOverview :entity="entity" />
    </template>
  </EntityDetail>
</template>
