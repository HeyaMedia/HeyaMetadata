<script setup lang="ts">
useSeoMeta({
  title: 'TV shows',
  description: 'Browse canonical television series with reconciled titles, seasons, artwork, and the provider evidence behind each match.',
  twitterCard: 'summary_large_image',
})
</script>

<template>
  <LibraryBrowser kind="tv_show" lock-kind title="Television" kicker="Series" />
</template>
