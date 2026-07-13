<script setup lang="ts">
import { FILTER_KINDS } from '~/utils/kinds'

// URL-driven search entry. Submitting navigates to /search?q=&kind=; the search
// page reads those params as its source of truth.
const props = withDefaults(defineProps<{
  initialQuery?: string
  initialKind?: string
  size?: 'bar' | 'hero'
}>(), { initialQuery: '', initialKind: '', size: 'bar' })

const query = ref(props.initialQuery)
const kind = ref(props.initialKind)

watch(() => props.initialQuery, value => { query.value = value })
watch(() => props.initialKind, value => { kind.value = value })

async function submit() {
  const q = query.value.trim()
  if (!q) return
  const params: Record<string, string> = { q }
  if (kind.value) params.kind = kind.value
  await navigateTo({ path: '/search', query: params })
}
</script>

<template>
  <form class="global-search" :class="`global-search--${size}`" role="search" @submit.prevent="submit">
    <svg class="global-search__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true">
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-4-4" />
    </svg>
    <input
      v-model="query"
      class="global-search__input"
      type="search"
      :placeholder="size === 'hero' ? 'Search movies, music, books, anime…' : 'Search the library'"
      aria-label="Search the metadata library"
    >
    <label class="global-search__kind">
      <span class="sr-only">Domain</span>
      <select v-model="kind" aria-label="Restrict search to a domain">
        <option value="">Everything</option>
        <option v-for="item in FILTER_KINDS" :key="item.kind" :value="item.kind">{{ item.plural }}</option>
      </select>
    </label>
    <button type="submit" class="global-search__submit" :disabled="!query.trim()">Search</button>
  </form>
</template>

<style scoped>
.global-search {
  display: grid;
  grid-template-columns: auto 1fr auto auto;
  align-items: center;
  overflow: hidden;
  border: 1px solid var(--line-strong);
  border-radius: var(--radius);
  background: #111619;
}
.global-search:focus-within { border-color: #6f643e; }
.global-search__icon { width: 1.05rem; margin-left: 0.85rem; color: #707b80; }
.global-search__input {
  min-width: 0;
  padding: 0.7rem 0.6rem;
  border: 0;
  outline: 0;
  background: transparent;
  color: var(--text);
  font-size: 0.8rem;
}
.global-search__input::placeholder { color: #586167; }
.global-search__kind select {
  height: 100%;
  padding: 0.55rem 0.7rem;
  border: 0;
  border-left: 1px solid var(--line);
  outline: 0;
  background: transparent;
  color: #929da0;
  font-size: 0.75rem;
}
.global-search__submit {
  align-self: stretch;
  padding: 0 1.1rem;
  border: 0;
  background: var(--gold);
  color: #18150c;
  font-size: 0.72rem;
  font-weight: 700;
}
.global-search__submit:disabled { opacity: 0.55; cursor: not-allowed; }

.global-search--hero { box-shadow: 0 1rem 3rem rgba(0, 0, 0, 0.28); }
.global-search--hero .global-search__input { padding-block: 0.95rem; font-size: 0.92rem; }

@media (max-width: 560px) {
  .global-search { grid-template-columns: auto 1fr; }
  .global-search__kind select, .global-search__submit {
    min-height: 2.7rem;
    border-top: 1px solid var(--line);
  }
  .global-search__kind { grid-column: 1 / 2; grid-row: 2; }
  .global-search__submit { grid-column: 2 / 3; grid-row: 2; }
}
</style>
