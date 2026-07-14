<script setup lang="ts">
useSeoMeta({
  title: 'Movies',
  description: 'Browse every film in the canonical library — reconciled titles, artwork, and the upstream sources behind each match.',
  twitterCard: 'summary_large_image',
})
</script>

<template>
  <LibraryBrowser kind="movie" lock-kind title="Movies" kicker="Films" />
</template>
