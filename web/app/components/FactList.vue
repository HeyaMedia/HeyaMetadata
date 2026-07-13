<script setup lang="ts">
// Definition list that is defensive by construction: every value flows through
// formatValue, and rows with an empty result are dropped. Callers can pass raw
// structured values without risking "[object Object]".
export interface Fact {
  label: string
  value: unknown
}

const props = defineProps<{ facts: Fact[] }>()

const rows = computed(() =>
  props.facts
    .map(fact => ({ label: fact.label, text: formatValue(fact.value) }))
    .filter(row => row.text !== ''),
)
</script>

<template>
  <dl v-if="rows.length" class="facts">
    <template v-for="row in rows" :key="row.label">
      <dt>{{ row.label }}</dt>
      <dd>{{ row.text }}</dd>
    </template>
  </dl>
  <p v-else class="muted facts-empty">No fields available.</p>
</template>

<style scoped>
.facts-empty { margin: 0; font-size: 0.76rem; }
</style>
