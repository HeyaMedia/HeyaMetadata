<script setup lang="ts">
</script>

<template>
  <footer class="app-footer">
    <div class="shell app-footer__inner">
      <span class="app-footer__brand">Heya v2</span>
      <p>One canonical truth, assembled from many imperfect sources.</p>
      <div class="app-footer__links">
        <a href="/api/docs" target="_blank" rel="noopener">API reference ↗</a>
        <a href="/api/openapi.json" target="_blank" rel="noopener">OpenAPI ↗</a>
      </div>
    </div>
  </footer>
</template>

<style scoped>
.app-footer { margin-top: 4rem; border-top: 1px solid var(--line); }
.app-footer__inner {
  display: flex;
  align-items: center;
  gap: 1.5rem;
  padding-block: 2rem;
  color: #626d72;
  font-size: 0.68rem;
}
.app-footer__brand { color: #8e999b; }
.app-footer__inner p { margin: 0; flex: 1 1 auto; }
.app-footer__links { display: flex; gap: 1.25rem; }
.app-footer__links a { color: #8e999b; }
.app-footer__links a:hover { color: #fff; }

@media (max-width: 640px) {
  .app-footer__inner { flex-direction: column; align-items: flex-start; gap: 0.75rem; }
}
</style>
