<script setup lang="ts">
const route = useRoute()
const id = computed(() => route.params.id as string)
</script>

<template>
  <EntityDetail :id="id" expected-kind="anime">
    <template #default="{ entity }">
      <TvAnimeOverview :entity="entity" />
    </template>
  </EntityDetail>
</template>
