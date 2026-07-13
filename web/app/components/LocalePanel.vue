<script setup lang="ts">
// Presentation locale — public (not account-gated). Drives title, description,
// and artwork selection for everyone browsing. Memory-only, forgotten on reload.
const { locale } = useLocale()
</script>

<template>
  <section class="locale-panel" aria-label="Presentation locale">
    <div class="locale-panel__intro">
      <span class="section-label">Presentation</span>
      <h2>Language &amp; region</h2>
      <p>Applied to titles, descriptions, and artwork selection. Held only in this page's memory.</p>
    </div>

    <fieldset class="locale-panel__fields">
      <label>
        <span>Language</span>
        <input v-model="locale.language" maxlength="16" autocomplete="off" placeholder="en">
      </label>
      <label>
        <span>Fallback languages</span>
        <input v-model="locale.fallbackLanguages" autocomplete="off" placeholder="ja, en">
      </label>
      <label>
        <span>Country</span>
        <input v-model="locale.country" maxlength="2" autocomplete="off" placeholder="DK">
      </label>
    </fieldset>
  </section>
</template>

<style scoped>
.locale-panel {
  display: grid;
  grid-template-columns: minmax(15rem, 0.7fr) 2fr;
  gap: clamp(1.5rem, 4vw, 3.5rem);
  align-items: center;
  padding: 1.75rem var(--shell-pad) 2rem;
  border-bottom: 1px solid var(--line);
  background: #101518;
}
.locale-panel__intro h2 { margin: 0.5rem 0; font-size: 1.25rem; font-weight: 500; }
.locale-panel__intro p { max-width: 30rem; margin: 0; color: var(--muted); font-size: 0.8rem; line-height: 1.7; }
.locale-panel__fields { display: grid; grid-template-columns: repeat(3, minmax(7rem, 1fr)); gap: 0.75rem; margin: 0; padding: 0; border: 0; }
label { display: block; }
label span {
  display: block;
  margin-bottom: 0.4rem;
  color: #788388;
  font-size: 0.62rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
input {
  width: 100%;
  padding: 0.6rem 0.7rem;
  border: 1px solid #2b3439;
  border-radius: var(--radius-sm);
  outline: none;
  background: #0b0f11;
  color: #dce1df;
  font-size: 0.74rem;
}
input:focus { border-color: #6f643e; }

@media (max-width: 860px) { .locale-panel { grid-template-columns: 1fr; align-items: start; } }
@media (max-width: 560px) { .locale-panel__fields { grid-template-columns: 1fr 1fr; } }
</style>
