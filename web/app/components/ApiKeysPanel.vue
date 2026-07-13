<script setup lang="ts">
import type { ApiKey } from '~/utils/types'

// Manage the user's Heya-issued API keys (Bearer access to the API). The full
// secret is returned only on creation and shown once here; the server stores
// only a hash + prefix. Revoke uses an inline confirm (no native dialog).
const auth = useAuth()

const keys = ref<ApiKey[]>([])
const loading = ref(true)
const error = ref('')

const newName = ref('')
const creating = ref(false)
const created = ref<ApiKey | null>(null)
const copied = ref(false)
const confirmingId = ref('')

async function load() {
  loading.value = true
  try {
    keys.value = await auth.listApiKeys()
  } catch (reason: any) {
    error.value = reason?.message || 'Could not load API keys'
  } finally {
    loading.value = false
  }
}
onMounted(load)

async function create() {
  const name = newName.value.trim()
  if (!name || creating.value) return
  creating.value = true
  error.value = ''
  try {
    created.value = await auth.createApiKey(name)
    newName.value = ''
    await load()
  } catch (reason: any) {
    error.value = reason?.message || 'Could not create key'
  } finally {
    creating.value = false
  }
}

async function copyKey() {
  if (!created.value?.key) return
  try {
    await navigator.clipboard.writeText(created.value.key)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1400)
  } catch { /* clipboard unavailable */ }
}

async function revoke(id: string) {
  if (confirmingId.value !== id) {
    confirmingId.value = id
    return
  }
  error.value = ''
  try {
    await auth.revokeApiKey(id)
    await load()
  } catch (reason: any) {
    error.value = reason?.message || 'Could not revoke key'
  } finally {
    confirmingId.value = ''
  }
}
</script>

<template>
  <section class="panel apikeys">
    <header class="apikeys__head">
      <div>
        <span class="section-label">Programmatic access</span>
        <h2>API keys</h2>
      </div>
    </header>

    <p class="apikeys__intro">
      Use an API key as <code>Authorization: Bearer heya_v2_…</code> to reach the API without a
      browser session. Revoking a key takes effect immediately.
    </p>

    <!-- Show a freshly created key once. -->
    <div v-if="created" class="apikeys__new">
      <strong>“{{ created.name }}” created.</strong>
      <p>Copy it now — it won't be shown again.</p>
      <div class="apikeys__secret">
        <code>{{ created.key }}</code>
        <button type="button" class="btn btn--sm" @click="copyKey">{{ copied ? 'Copied ✓' : 'Copy' }}</button>
      </div>
      <button type="button" class="btn--link apikeys__dismiss" @click="created = null">Done</button>
    </div>

    <form class="apikeys__create" @submit.prevent="create">
      <input v-model="newName" type="text" maxlength="80" placeholder="Key name (e.g. Living Room Server)" aria-label="API key name">
      <button type="submit" class="btn btn--gold btn--sm" :disabled="!newName.trim() || creating">
        {{ creating ? 'Creating…' : 'Create key' }}
      </button>
    </form>

    <div v-if="error" class="notice apikeys__error"><span>{{ error }}</span></div>

    <p v-if="loading" class="muted apikeys__status">Loading keys…</p>
    <ul v-else-if="keys.length" class="apikeys__list">
      <li v-for="key in keys" :key="key.id">
        <span class="apikeys__meta">
          <strong>{{ key.name }}</strong>
          <span>{{ [key.prefix, formatDate(key.created_at)].filter(Boolean).join(' · ') }}</span>
        </span>
        <button
          type="button"
          class="btn btn--sm apikeys__revoke"
          :class="{ 'is-confirming': confirmingId === key.id }"
          @click="revoke(key.id)"
          @blur="confirmingId = ''"
        >
          {{ confirmingId === key.id ? 'Confirm revoke' : 'Revoke' }}
        </button>
      </li>
    </ul>
    <p v-else class="muted apikeys__status">No API keys yet.</p>
  </section>
</template>

<style scoped>
.apikeys { margin-top: 1rem; }
.apikeys__head { margin-bottom: 0.75rem; }
.apikeys__head h2 { margin: 0.35rem 0 0; font-size: 1.05rem; font-weight: 500; }
.apikeys__intro { margin: 0 0 1.25rem; color: var(--muted); font-size: 0.8rem; line-height: 1.6; }
.apikeys__intro code { color: var(--gold); font-family: var(--font-mono); font-size: 0.74rem; }

.apikeys__new {
  margin-bottom: 1.25rem;
  padding: 1rem;
  border: 1px solid #5f593e;
  border-radius: var(--radius-sm);
  background: rgba(241, 201, 107, 0.05);
}
.apikeys__new strong { font-size: 0.85rem; }
.apikeys__new p { margin: 0.25rem 0 0.75rem; color: var(--muted); font-size: 0.74rem; }
.apikeys__secret { display: flex; align-items: center; gap: 0.6rem; }
.apikeys__secret code {
  flex: 1 1 auto;
  overflow: hidden;
  padding: 0.55rem 0.7rem;
  border: 1px solid var(--line-strong);
  border-radius: var(--radius-sm);
  background: var(--panel-2);
  color: var(--text);
  font-family: var(--font-mono);
  font-size: 0.72rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.apikeys__dismiss { margin-top: 0.7rem; color: var(--muted); }

.apikeys__create { display: flex; gap: 0.6rem; margin-bottom: 1rem; }
.apikeys__create input {
  flex: 1 1 auto;
  min-width: 0;
  padding: 0.6rem 0.75rem;
  border: 1px solid var(--line-strong);
  border-radius: var(--radius-sm);
  outline: none;
  background: var(--panel-2);
  color: var(--text);
  font-size: 0.8rem;
}
.apikeys__create input:focus { border-color: #6f643e; }

.apikeys__error { margin: 0 0 1rem; }
.apikeys__status { margin: 0.5rem 0 0; font-size: 0.8rem; }
.apikeys__list { display: flex; flex-direction: column; margin: 0; padding: 0; list-style: none; }
.apikeys__list li {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.7rem 0;
  border-top: 1px solid var(--line-soft);
}
.apikeys__list li:first-child { border-top: 0; }
.apikeys__meta { display: flex; flex-direction: column; gap: 0.15rem; min-width: 0; }
.apikeys__meta strong { font-size: 0.82rem; font-weight: 500; }
.apikeys__meta span { color: var(--muted-2); font-family: var(--font-mono); font-size: 0.66rem; }
.apikeys__revoke.is-confirming { border-color: var(--danger); color: var(--danger); }
</style>
