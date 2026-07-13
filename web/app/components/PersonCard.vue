<script setup lang="ts">
// Cast/crew credit. Links to the person's filmography page when a provider
// person id is available; otherwise presentational. Portrait crop.
const props = defineProps<{
  name?: string
  role?: string
  imageId?: string
  to?: string
}>()

const displayName = computed(() => formatValue(props.name) || 'Unknown')
const roleText = computed(() => formatValue(props.role))
// A string `:is="'NuxtLink'"` does not resolve the auto-imported component at
// runtime; the resolved reference does.
const linkTag = resolveComponent('NuxtLink')
</script>

<template>
  <component :is="to ? linkTag : 'figure'" :to="to" class="person" :class="{ 'is-link': to }">
    <span class="person__art">
      <MetadataImage :image-id="imageId" :alt="displayName" variant="thumb" />
    </span>
    <span class="person__body">
      <strong class="person__name">{{ displayName }}</strong>
      <span v-if="roleText" class="person__role">{{ roleText }}</span>
    </span>
  </component>
</template>

<style scoped>
.person {
  display: flex;
  flex-direction: column;
  min-width: 0;
  margin: 0;
  overflow: hidden;
  border: 1px solid var(--line-soft);
  border-radius: var(--radius);
  background: var(--panel);
}
.person.is-link { transition: transform 0.18s ease, border-color 0.18s ease; }
.person.is-link:hover { transform: translateY(-3px); border-color: #5a5236; }
.person__art { aspect-ratio: 3 / 4; width: 100%; overflow: hidden; background: #12171c; }
.person__body { display: flex; flex-direction: column; padding: 0.6rem 0.7rem 0.75rem; }
.person__name {
  overflow: hidden;
  font-size: 0.78rem;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.person__role {
  margin-top: 0.2rem;
  overflow: hidden;
  color: var(--muted-2);
  font-size: 0.66rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
