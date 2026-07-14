<script setup lang="ts">
// Intentionally minimal for now — the handbook is still being written. The live
// API reference is real, so it's the one link worth surfacing today.
useSeoMeta({
  title: 'Docs',
  description: 'Guides and references for installing and operating HeyaMediaServer, plus the live API reference.',
  twitterCard: 'summary_large_image',
})
</script>

<template>
  <div class="shell page docs">
    <header class="page-heading">
      <div>
        <span class="section-label">Docs</span>
        <h1 class="editorial">Everything you need to <em>run Heya.</em></h1>
        <p>Guides and references for installing and operating HeyaMediaServer.</p>
      </div>
    </header>

    <div class="placeholder">
      <span class="section-label">Coming soon</span>
      <h2>The handbook is being written.</h2>
      <p>Setup, configuration, and operations guides are on the way. In the meantime, the interactive API reference is live and the source is on GitHub.</p>
      <div class="placeholder__actions">
        <a href="/api/docs" target="_blank" rel="noopener noreferrer" class="btn btn--gold">Open API reference ↗</a>
        <a href="https://github.com/HeyaMedia/Heya" target="_blank" rel="noopener noreferrer" class="btn btn--ghost">Source on GitHub ↗</a>
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
