<script setup lang="ts">
// Tab bar whose active section lives in the URL (?tab=), so browser back/forward
// restore the section. Overview is the default and carries no query param.
const props = defineProps<{
  tabs: { key: string; label: string; count?: number }[]
}>()

const route = useRoute()
const active = computed(() => (route.query.tab as string) || 'overview')

function select(key: string) {
  const query = { ...route.query }
  if (key === 'overview') delete query.tab
  else query.tab = key
  navigateTo({ path: route.path, query })
}
</script>

<template>
  <nav class="tabs" role="tablist" aria-label="Entity sections">
    <button
      v-for="tab in props.tabs"
      :key="tab.key"
      type="button"
      role="tab"
      :aria-selected="active === tab.key"
      class="tabs__tab"
      :class="{ 'is-active': active === tab.key }"
      @click="select(tab.key)"
    >
      {{ tab.label }}
      <span v-if="tab.count != null" class="tabs__count">{{ tab.count }}</span>
    </button>
  </nav>
</template>

<style scoped>
.tabs {
  display: flex;
  gap: 1.75rem;
  margin-top: 2.5rem;
  overflow-x: auto;
  border-bottom: 1px solid var(--line);
  scrollbar-width: none;
}
.tabs::-webkit-scrollbar { display: none; }
.tabs__tab {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.9rem 0;
  border: 0;
  background: none;
  color: #758086;
  font-size: 0.76rem;
  white-space: nowrap;
}
.tabs__tab:hover { color: #cfd4d1; }
.tabs__tab.is-active { color: #fff; }
.tabs__tab.is-active::after {
  content: '';
  position: absolute;
  inset-inline: 0;
  bottom: -1px;
  height: 1px;
  background: var(--gold);
}
.tabs__count {
  padding: 0.05rem 0.4rem;
  border-radius: 1rem;
  background: #1b2226;
  color: #8a949a;
  font-family: var(--font-mono);
  font-size: 0.6rem;
}
</style>
