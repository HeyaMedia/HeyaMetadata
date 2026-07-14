<script setup lang="ts">
useSeoMeta({
  title: 'Books',
  description: 'Browse canonical book works with reconciled titles, editions, cover art, and the upstream records behind each match.',
  twitterCard: 'summary_large_image',
})
</script>

<template>
  <LibraryBrowser kind="book_work" lock-kind title="Books" kicker="Works" />
</template>
