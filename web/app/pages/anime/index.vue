<script setup lang="ts">
useSeoMeta({
  title: 'Anime',
  description: 'Browse the anime catalog with reconciled titles, artwork, and every upstream source attached to each canonical entry.',
  twitterCard: 'summary_large_image',
})
</script>

<template>
  <LibraryBrowser kind="anime" lock-kind title="Anime" kicker="Series" />
</template>
