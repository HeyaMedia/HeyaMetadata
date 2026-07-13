<script setup lang="ts">
const route = useRoute()
const id = computed(() => route.params.id as string)
</script>

<template>
  <EntityDetail :id="id" expected-kind="release">
    <template #default="{ entity }">
      <ReleaseOverview :entity="entity" />
    </template>
  </EntityDetail>
</template>
