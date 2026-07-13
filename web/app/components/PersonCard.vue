<script setup lang="ts">
// Cast/crew credit. These are provider persons without canonical detail pages,
// so the card is presentational (no link). Portrait crop.
const props = defineProps<{
  name?: string
  role?: string
  imageId?: string
}>()

const displayName = computed(() => formatValue(props.name) || 'Unknown')
const roleText = computed(() => formatValue(props.role))
</script>

<template>
  <figure class="person">
    <span class="person__art">
      <MetadataImage :image-id="imageId" :alt="displayName" variant="thumb" />
    </span>
    <figcaption class="person__body">
      <strong class="person__name">{{ displayName }}</strong>
      <span v-if="roleText" class="person__role">{{ roleText }}</span>
    </figcaption>
  </figure>
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
