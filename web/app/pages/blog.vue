<script setup lang="ts">
// Intentionally minimal for now — no posts yet. Release notes live on GitHub
// until there's something worth writing up here.
useSeoMeta({
  title: 'Blog',
  description: 'Release notes, deep dives, and the occasional opinion on getting media metadata right — the first posts are on the way.',
  twitterCard: 'summary_large_image',
})
</script>

<template>
  <div class="shell page blog">
    <header class="page-heading">
      <div>
        <span class="section-label">Blog</span>
        <h1 class="editorial">Notes from the <em>workshop.</em></h1>
        <p>Release notes, deep dives, and the occasional opinion on getting media right.</p>
      </div>
    </header>

    <div class="placeholder">
      <span class="section-label">Coming soon</span>
      <h2>Nothing published yet.</h2>
      <p>The first posts are still being written. Until then, releases and day-to-day changes live on GitHub.</p>
      <div class="placeholder__actions">
        <a href="https://github.com/HeyaMedia/Heya/releases" target="_blank" rel="noopener noreferrer" class="btn btn--gold">Releases on GitHub ↗</a>
        <NuxtLink to="/features" class="btn btn--ghost">Explore features</NuxtLink>
      </div>
    </div>
  </div>
</template>

<style scoped>
.placeholder {
  max-width: 42rem;
  padding: 2.5rem;
  border: 1px solid var(--line);
  border-radius: var(--radius);
  background: rgba(17, 22, 25, 0.6);
}
.placeholder h2 { margin: 0.7rem 0 0.6rem; font-size: 1.5rem; font-weight: 500; letter-spacing: -0.02em; }
.placeholder p { margin: 0 0 1.5rem; color: var(--muted); font-size: 0.85rem; line-height: 1.7; }
.placeholder__actions { display: flex; flex-wrap: wrap; gap: 0.75rem; }
</style>
