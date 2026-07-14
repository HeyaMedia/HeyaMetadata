<script setup lang="ts">
useSeoMeta({
  title: 'Comics',
  description: 'Browse canonical comic volumes with reconciled titles, cover art, and the upstream evidence behind each match.',
  twitterCard: 'summary_large_image',
})
</script>

<template>
  <LibraryBrowser kind="comic_volume" lock-kind title="Comics" kicker="Volumes" />
</template>
