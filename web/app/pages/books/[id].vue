<script setup lang="ts">
const route = useRoute()
const id = computed(() => route.params.id as string)
</script>

<template>
  <EntityDetail :id="id" expected-kind="book_work">
    <template #default="{ entity }">
      <BookOverview :entity="entity" />
    </template>
  </EntityDetail>
</template>
