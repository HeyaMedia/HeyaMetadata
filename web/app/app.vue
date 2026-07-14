<script setup lang="ts">
// The whole app is file-based now: layouts/default.vue provides the header +
// footer shell, and each route renders through <NuxtPage />.
useHead({
  htmlAttrs: { lang: 'en' },
})

// Restore the session (if any) from the httpOnly cookie on load.
onMounted(() => useAuth().hydrate())
</script>

<template>
  <NuxtLayout>
    <NuxtPage />
  </NuxtLayout>
  <SongPanel />
</template>
