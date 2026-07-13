<script setup lang="ts">
// Request-scoped provider keys (account-gated). Held only in page memory (see
// useProviderCredentials), sent as Heya request headers, never persisted.
const { fields, credentials } = useProviderCredentials()
</script>

<template>
  <section class="keys-panel" aria-label="Provider keys">
    <div class="keys-panel__intro">
      <span class="section-label">Request-scoped access</span>
      <h2>Provider keys</h2>
      <p>
        Bring your own provider keys for discovery and refresh. Held only in this page's
        memory, sent directly as Heya request headers, and forgotten on reload.
      </p>
    </div>

    <div class="keys-panel__body">
      <fieldset class="keys-panel__grid">
        <label v-for="field in fields" :key="field.key">
          <span>{{ field.label }}</span>
          <input
            v-model="credentials[field.key]"
            type="password"
            autocomplete="off"
            placeholder="Not configured"
          >
        </label>
      </fieldset>
    </div>
  </section>
</template>

<style scoped>
.keys-panel {
  display: grid;
  grid-template-columns: minmax(15rem, 0.7fr) 2fr;
  gap: clamp(1.5rem, 4vw, 3.5rem);
  padding: 1.75rem var(--shell-pad) 2.25rem;
  border-bottom: 1px solid var(--line);
  background: #101518;
}
.keys-panel__intro h2 { margin: 0.5rem 0; font-size: 1.3rem; font-weight: 500; }
.keys-panel__intro p { max-width: 30rem; margin: 0; color: var(--muted); font-size: 0.8rem; line-height: 1.7; }
.keys-panel__body { display: flex; flex-direction: column; gap: 1.5rem; }
fieldset { min-width: 0; margin: 0; padding: 0; border: 0; }
legend { margin-bottom: 0.75rem; padding: 0; }
.keys-panel__locale { display: grid; grid-template-columns: repeat(3, minmax(7rem, 1fr)); gap: 0.75rem; }
.keys-panel__grid { display: grid; grid-template-columns: repeat(3, minmax(9rem, 1fr)); gap: 0.75rem; }
.keys-panel__locale legend, .keys-panel__grid legend { grid-column: 1 / -1; }
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
.keys-panel__grid input { font-family: var(--font-mono); font-size: 0.7rem; }
input:focus { border-color: #6f643e; }

@media (max-width: 860px) {
  .keys-panel { grid-template-columns: 1fr; }
}
@media (max-width: 560px) {
  .keys-panel__locale, .keys-panel__grid { grid-template-columns: 1fr 1fr; }
}
</style>
