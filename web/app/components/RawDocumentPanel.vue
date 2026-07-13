<script setup lang="ts">
import type { EntityDocument } from '~/utils/types'

// The only place formatted JSON is acceptable. Everything else goes through the
// display helpers.
const props = defineProps<{ entity: EntityDocument }>()

const json = computed(() => JSON.stringify(props.entity, null, 2))
const copied = ref(false)

async function copy() {
  try {
    await navigator.clipboard.writeText(json.value)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1400)
  } catch {
    /* clipboard unavailable */
  }
}
</script>

<template>
  <div class="raw">
    <header class="raw__head">
      <div>
        <span class="section-label">Canonical document</span>
        <h2>Raw JSON</h2>
      </div>
      <div class="raw__meta">
        <span v-if="entity.projection_version != null">Projection {{ entity.projection_version }}</span>
        <button type="button" class="btn btn--sm btn--ghost" @click="copy">{{ copied ? 'Copied ✓' : 'Copy JSON' }}</button>
      </div>
    </header>
    <pre><code>{{ json }}</code></pre>
  </div>
</template>

<style scoped>
.raw__head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1.1rem;
}
.raw__head h2 { margin: 0.4rem 0 0; font-size: 1.35rem; font-weight: 500; }
.raw__meta { display: flex; align-items: center; gap: 1rem; }
.raw__meta > span { color: var(--muted-2); font-family: var(--font-mono); font-size: 0.62rem; }
pre {
  overflow: auto;
  max-height: 65vh;
  margin: 0;
  padding: 1.5rem;
  border: 1px solid var(--line);
  border-radius: var(--radius);
  background: #0a0d0f;
}
code { color: #aabdb5; font: 0.68rem / 1.7 var(--font-mono); }
</style>
