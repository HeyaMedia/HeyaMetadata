<script setup lang="ts">
// The whole app is file-based now: layouts/default.vue provides the header +
// footer shell, and each route renders through <NuxtPage />.
useHead({
  htmlAttrs: { lang: 'en' },
})
</script>

<template>
  <NuxtLayout>
    <NuxtPage />
  </NuxtLayout>
</template>
