<script setup lang="ts">
const route = useRoute()
const id = computed(() => route.params.id as string)
</script>

<template>
  <EntityDetail :id="id" expected-kind="manga">
    <template #default="{ entity }">
      <MangaOverview :entity="entity" />
    </template>
  </EntityDetail>
</template>
