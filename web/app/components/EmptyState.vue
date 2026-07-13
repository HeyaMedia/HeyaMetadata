<script setup lang="ts">
withDefaults(defineProps<{
  title: string
  message?: string
}>(), { message: '' })
</script>

<template>
  <div class="empty-state">
    <span class="empty-orbit" aria-hidden="true"><i /><i /></span>
    <h3>{{ title }}</h3>
    <p v-if="message">{{ message }}</p>
    <div v-if="$slots.default" class="empty-state__action">
      <slot />
    </div>
  </div>
</template>

<style scoped>
.empty-orbit {
  position: relative;
  width: 3rem;
  height: 3rem;
  border: 1px solid #303b3f;
  border-radius: 50%;
}
.empty-orbit i:first-child { position: absolute; inset: 0.65rem; border: 1px solid #5f593e; border-radius: 50%; }
.empty-orbit i:last-child {
  position: absolute;
  top: -0.15rem;
  left: 1.35rem;
  width: 0.3rem;
  height: 0.3rem;
  border-radius: 50%;
  background: var(--gold);
}
.empty-state__action { margin-top: 1.4rem; }
</style>
