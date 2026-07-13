<script setup lang="ts">
const route = useRoute()
const id = computed(() => route.params.id as string)
</script>

<template>
  <EntityDetail :id="id" expected-kind="tv_show">
    <template #default="{ entity }">
      <TvAnimeOverview :entity="entity" />
    </template>
  </EntityDetail>
</template>
