<script setup lang="ts">
useSeoMeta({
  title: 'Manga',
  description: 'Browse canonical manga series with reconciled titles, volumes, cover art, and every upstream source still attached.',
  twitterCard: 'summary_large_image',
})
</script>

<template>
  <LibraryBrowser kind="manga" lock-kind title="Manga" kicker="Series" />
</template>
