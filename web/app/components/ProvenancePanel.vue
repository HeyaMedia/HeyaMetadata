<script setup lang="ts">
import type { EntityDocument } from '~/utils/types'

// Where each claim came from. provenance is a map of scope → source[] where
// each source carries a provider and an observation id.
const props = defineProps<{ entity: EntityDocument }>()

const scopes = computed(() => Object.entries(props.entity.provenance ?? {}).filter(([, sources]) => Array.isArray(sources) && sources.length))
</script>

<template>
  <div class="provenance">
    <header class="provenance__intro">
      <span class="section-label">Receipts included</span>
      <h2>Where each claim came from</h2>
    </header>

    <article v-for="[scope, sources] in scopes" :key="scope" class="provenance__scope">
      <h3>{{ formatKey(scope) }}</h3>
      <ul>
        <li v-for="(source, index) in sources" :key="source.observation_id || index">
          <span class="provenance__provider">{{ formatValue(source.provider) || 'unknown' }}</span>
          <code v-if="source.observation_id">{{ source.observation_id }}</code>
        </li>
      </ul>
    </article>

    <EmptyState
      v-if="!scopes.length"
      title="No projected provenance."
      message="The canonical record exists, but its public provenance projection needs attention."
    />
  </div>
</template>

<style scoped>
.provenance__intro { margin-bottom: 1.75rem; }
.provenance__intro h2 { margin: 0.4rem 0 0; font-size: 1.35rem; font-weight: 500; }
.provenance__scope {
  display: grid;
  grid-template-columns: minmax(9rem, 0.4fr) 1.6fr;
  gap: 1rem;
  padding: 1.15rem 0;
  border-top: 1px solid var(--line);
}
.provenance__scope h3 { margin: 0; font-size: 0.82rem; font-weight: 500; }
.provenance__scope ul { display: flex; flex-direction: column; gap: 0.4rem; margin: 0; padding: 0; list-style: none; }
.provenance__scope li { display: grid; grid-template-columns: 8rem 1fr; gap: 0.6rem; align-items: baseline; }
.provenance__provider { color: var(--gold); font-size: 0.72rem; text-transform: capitalize; }
.provenance__scope code { overflow: hidden; color: #738086; font-family: var(--font-mono); font-size: 0.62rem; text-overflow: ellipsis; }

@media (max-width: 640px) {
  .provenance__scope { grid-template-columns: 1fr; gap: 0.5rem; }
  .provenance__scope li { grid-template-columns: 1fr; gap: 0.15rem; }
}
</style>
