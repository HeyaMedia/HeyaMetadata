<script setup lang="ts">
import type { ExternalId } from '~/utils/types'

const props = defineProps<{ externalIds?: ExternalId[] }>()

const ids = computed(() => (props.externalIds ?? []).filter(external => formatValue(external.value)))
</script>

<template>
  <OverviewPanel title="External IDs" kicker="Identity spine">
    <div v-if="ids.length" class="external-ids">
      <div v-for="external in ids" :key="`${external.provider}:${external.namespace}:${external.value}`" class="external-id">
        <span class="external-id__provider">{{ formatKey(external.provider || '') }}</span>
        <small v-if="external.namespace">{{ external.namespace }}</small>
        <code>{{ external.value }}</code>
      </div>
    </div>
    <p v-else class="muted">No external IDs exposed.</p>
  </OverviewPanel>
</template>

<style scoped>
.external-ids { display: flex; flex-direction: column; }
.external-id {
  display: grid;
  grid-template-columns: 0.8fr 0.7fr 1.5fr;
  gap: 0.6rem;
  align-items: baseline;
  padding: 0.6rem 0;
  border-top: 1px solid var(--line-soft);
}
.external-id:first-child { border-top: 0; }
.external-id__provider { color: var(--gold); font-size: 0.72rem; }
.external-id small { color: var(--muted-2); font-size: 0.62rem; }
.external-id code {
  overflow: hidden;
  color: #aeb6b3;
  font-family: var(--font-mono);
  font-size: 0.64rem;
  text-overflow: ellipsis;
}
.muted { font-size: 0.76rem; }
</style>
