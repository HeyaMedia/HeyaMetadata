<script setup lang="ts">
const route = useRoute()
const id = computed(() => route.params.id as string)
</script>

<template>
  <EntityDetail :id="id" expected-kind="recording">
    <template #default="{ entity }">
      <RecordingOverview :entity="entity" />
    </template>
  </EntityDetail>
</template>
