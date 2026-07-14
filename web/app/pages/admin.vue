<script setup lang="ts">
import type { AdminJob, AdminJobAction } from '~/utils/types'

useSeoMeta({ robots: 'noindex, nofollow', title: 'Admin' })

// Admin-only ops view. Primary purpose: watch and manage the River job queue.
// Access is enforced by the backend (403 for non-admins); the UI also gates on
// the session role so non-admins get a clean message instead of a failed fetch.
const api = useHeyaApi()
const { isAdmin, ready, hydrate } = useAuth()
await hydrate()

const state = ref('')
const kind = ref('')
const autoRefresh = ref(true)
const lastUpdated = ref<number | null>(null)
const openId = ref<number | null>(null)

const { data, pending, error, refresh } = await useAsyncData(
  'admin-jobs',
  async () => {
    if (!isAdmin.value) return null
    const result = await api.adminJobs({ state: state.value, kind: kind.value, limit: 100 })
    lastUpdated.value = Date.now()
    return result
  },
  { watch: [state, kind, isAdmin], default: () => null },
)

const summary = computed(() => data.value?.summary ?? [])
const jobs = computed(() => data.value?.jobs ?? [])
const total = computed(() => data.value?.total ?? 0)
function stateCount(...names: string[]) {
  return summary.value.filter(row => names.includes(row.state)).reduce((sum, row) => sum + row.count, 0)
}

const STATE_ORDER = ['running', 'retryable', 'available', 'scheduled', 'pending', 'completed', 'cancelled', 'discarded']
const orderedSummary = computed(() =>
  [...summary.value].sort((a, b) => (STATE_ORDER.indexOf(a.state) + 1 || 99) - (STATE_ORDER.indexOf(b.state) + 1 || 99)),
)

const DANGER = new Set(['discarded', 'cancelled'])
const ACTIVE = new Set(['running', 'retryable'])
function tone(s: string): 'good' | 'warn' | 'bad' | 'idle' {
  if (ACTIVE.has(s)) return 'good'
  if (DANGER.has(s)) return 'bad'
  if (s === 'available' || s === 'scheduled' || s === 'pending') return 'warn'
  return 'idle'
}

function ago(ts?: string): string {
  if (!ts) return ''
  const t = new Date(ts).getTime()
  if (!Number.isFinite(t)) return ''
  const s = Math.max(0, Math.round((Date.now() - t) / 1000))
  if (s < 60) return `${s}s ago`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}
function jobWhen(job: AdminJob): string {
  return ago(job.finalized_at || job.attempted_at || job.scheduled_at)
}
const STUCK_SECONDS = 15 * 60
function runningSeconds(job: AdminJob): number {
  if (job.state !== 'running' || !job.attempted_at) return -1
  return Math.max(0, Math.round((Date.now() - new Date(job.attempted_at).getTime()) / 1000))
}
function runningFor(job: AdminJob): string {
  const s = runningSeconds(job)
  if (s < 0) return ''
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${s % 60}s`
  const h = Math.floor(m / 60)
  return `${h}h ${m % 60}m`
}
function toggleRow(id: number) {
  openId.value = openId.value === id ? null : id
}

// ---- Bulk actions ---------------------------------------------------------
const confirming = ref<AdminJobAction | null>(null)
const actionBusy = ref(false)
const actionResult = ref('')

const ACTION_LABELS: Record<AdminJobAction, string> = {
  rescue_stuck: 'Rescue stuck',
  clear_completed: 'Clear completed',
  clear_queue: 'Clear queue',
}
const confirmPrompt = computed(() => {
  switch (confirming.value) {
    case 'rescue_stuck': return 'Requeue every job stuck running longer than 15 minutes?'
    case 'clear_completed': return `Delete ${formatCount(stateCount('completed'))} completed job records? This can't be undone.`
    case 'clear_queue': return `Delete ${formatCount(stateCount('available', 'scheduled', 'retryable', 'pending'))} waiting jobs? This cancels pending ingestion and can't be undone.`
    default: return ''
  }
})

function askAction(action: AdminJobAction) {
  actionResult.value = ''
  confirming.value = action
}
async function runAction() {
  if (!confirming.value) return
  const action = confirming.value
  actionBusy.value = true
  try {
    const result = await api.adminJobAction(action)
    actionResult.value = `${ACTION_LABELS[action]}: ${formatCount(result.affected)} ${result.affected === 1 ? 'job' : 'jobs'} affected.`
    confirming.value = null
    await refresh()
  } catch (reason: any) {
    actionResult.value = reason?.message || 'Action failed'
  } finally {
    actionBusy.value = false
  }
}

// Auto-refresh while visible; pauses in a background tab to avoid wasted polls.
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  timer = setInterval(() => {
    if (autoRefresh.value && isAdmin.value && !document.hidden && !confirming.value) refresh()
  }, 4000)
})
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>

<template>
  <div class="shell page">
    <header class="page-heading">
      <div>
        <span class="section-label">Operations</span>
        <h1>Job queue</h1>
        <p v-if="isAdmin && total">{{ formatCount(total) }} jobs in River<template v-if="lastUpdated"> · updated {{ ago(new Date(lastUpdated).toISOString()) }}</template></p>
      </div>
    </header>

    <div v-if="!ready" class="progress-line"><span class="spinner" /><p>Checking access…</p></div>

    <EmptyState
      v-else-if="!isAdmin"
      title="Admins only."
      message="This area is restricted to accounts with the admin role."
    >
      <NuxtLink to="/" class="btn btn--ghost">← Back to library</NuxtLink>
    </EmptyState>

    <template v-else>
      <!-- Queue state summary -->
      <div class="job-summary">
        <button type="button" class="job-stat" :class="{ 'is-active': state === '' }" @click="state = ''">
          <span class="job-stat__count">{{ formatCount(total) }}</span>
          <span class="job-stat__label">All jobs</span>
        </button>
        <button
          v-for="row in orderedSummary"
          :key="row.state"
          type="button"
          class="job-stat"
          :class="[`tone-${tone(row.state)}`, { 'is-active': state === row.state }]"
          @click="state = state === row.state ? '' : row.state"
        >
          <span class="job-stat__count">{{ formatCount(row.count) }}</span>
          <span class="job-stat__label"><i class="dot" />{{ row.state }}</span>
        </button>
      </div>

      <!-- Bulk actions -->
      <div class="job-actions">
        <button type="button" class="btn btn--sm" @click="askAction('rescue_stuck')">Rescue stuck</button>
        <button type="button" class="btn btn--sm" @click="askAction('clear_completed')">Clear completed</button>
        <button type="button" class="btn btn--sm job-actions__danger" @click="askAction('clear_queue')">Clear queue</button>
        <span v-if="actionResult" class="job-actions__result">{{ actionResult }}</span>
      </div>
      <div v-if="confirming" class="job-confirm">
        <span>{{ confirmPrompt }}</span>
        <div class="job-confirm__buttons">
          <button type="button" class="btn btn--sm btn--gold" :disabled="actionBusy" @click="runAction">{{ actionBusy ? 'Working…' : 'Confirm' }}</button>
          <button type="button" class="btn btn--sm btn--ghost" :disabled="actionBusy" @click="confirming = null">Cancel</button>
        </div>
      </div>

      <!-- Filter + refresh -->
      <div class="job-controls">
        <input v-model="kind" type="search" class="job-filter" placeholder="Filter by kind (e.g. artist_catalog)…" aria-label="Filter by job kind">
        <label class="job-auto"><input v-model="autoRefresh" type="checkbox"> Auto-refresh</label>
        <button type="button" class="btn btn--sm" :disabled="pending" @click="refresh">{{ pending ? 'Refreshing…' : 'Refresh' }}</button>
      </div>

      <div v-if="error" class="notice"><strong>That didn't work.</strong><span>{{ error }}</span></div>

      <!-- Jobs table -->
      <div class="job-table">
        <div class="job-row job-row--head">
          <span>State</span><span>Kind</span><span>ID</span><span>Attempt</span><span>Running for</span><span>Updated</span><span>Last error</span>
        </div>
        <div v-for="job in jobs" :key="job.id" class="job-item" :class="{ 'is-open': openId === job.id }">
          <button type="button" class="job-row job-row--click" @click="toggleRow(job.id)">
            <span><span class="badge" :class="`tone-${tone(job.state)}`">{{ job.state }}</span></span>
            <span class="job-kind">{{ job.kind }}</span>
            <span class="job-id">{{ job.id }}</span>
            <span class="job-attempt" :class="{ 'is-exhausted': job.attempt >= job.max_attempts && DANGER.has(job.state) }">{{ job.attempt }}/{{ job.max_attempts }}</span>
            <span class="job-running" :class="{ 'is-stuck': runningSeconds(job) > STUCK_SECONDS }">{{ runningFor(job) || '—' }}</span>
            <span class="job-when">{{ jobWhen(job) }}</span>
            <span class="job-error" :title="job.error || ''">{{ job.error || '—' }}</span>
          </button>
          <div v-if="openId === job.id" class="job-detail">
            <div class="job-detail__meta">
              <span><b>queue</b> {{ job.queue }}</span>
              <span><b>priority</b> {{ job.priority }}</span>
              <span><b>created</b> {{ ago(job.created_at) }}</span>
              <span v-if="job.attempted_at"><b>attempted</b> {{ ago(job.attempted_at) }}</span>
              <span v-if="job.finalized_at"><b>finalized</b> {{ ago(job.finalized_at) }}</span>
            </div>
            <div v-if="job.error" class="job-detail__error">{{ job.error }}</div>
            <div class="job-detail__args">
              <span class="section-label">args</span>
              <pre><code>{{ JSON.stringify(job.args ?? {}, null, 2) }}</code></pre>
            </div>
          </div>
        </div>
        <EmptyState v-if="!jobs.length && !pending" title="No jobs match." message="Nothing in the queue for this filter." />
      </div>
    </template>
  </div>
</template>

<style scoped>
.progress-line { display: flex; align-items: center; gap: 0.75rem; padding: 2rem 0; color: var(--muted); font-size: 0.85rem; }

.job-summary { display: grid; grid-template-columns: repeat(auto-fill, minmax(130px, 1fr)); gap: 0.75rem; margin-bottom: 1.25rem; }
.job-stat {
  display: flex; flex-direction: column; gap: 0.35rem;
  padding: 0.9rem 1rem; border: 1px solid var(--line); border-radius: var(--radius);
  background: var(--panel); text-align: left; cursor: pointer;
  transition: border-color 0.15s ease, background 0.15s ease;
}
.job-stat:hover { border-color: var(--line-strong); }
.job-stat.is-active { border-color: var(--gold); background: rgba(241, 201, 107, 0.08); }
.job-stat__count { font-family: var(--font-mono); font-size: 1.5rem; }
.job-stat__label { display: flex; align-items: center; gap: 0.4rem; color: var(--muted-2); font-size: 0.66rem; text-transform: capitalize; }
.tone-good { --tone: var(--green); }
.tone-warn { --tone: var(--gold); }
.tone-bad { --tone: var(--danger); }
.tone-idle { --tone: var(--muted-2); }
.job-stat .dot { width: 0.45rem; height: 0.45rem; border-radius: 50%; background: var(--tone, var(--muted-2)); }

.job-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 0.6rem; margin-bottom: 0.75rem; }
.job-actions__danger { border-color: color-mix(in srgb, var(--danger) 40%, var(--line-strong)); color: #d99b93; }
.job-actions__danger:hover { border-color: var(--danger); color: var(--danger); }
.job-actions__result { color: var(--muted); font-size: 0.72rem; }
.job-confirm {
  display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 0.75rem;
  margin-bottom: 1.25rem; padding: 0.8rem 1rem;
  border: 1px solid color-mix(in srgb, var(--danger) 35%, var(--line)); border-radius: var(--radius);
  background: rgba(227, 113, 99, 0.06); color: var(--text-dim); font-size: 0.76rem;
}
.job-confirm__buttons { display: flex; gap: 0.5rem; }

.job-controls { display: flex; flex-wrap: wrap; align-items: center; gap: 0.75rem; margin-bottom: 1.25rem; }
.job-filter { flex: 1 1 20rem; padding: 0.55rem 0.8rem; border: 1px solid var(--line-strong); border-radius: var(--radius-sm); background: var(--panel); color: var(--text); font-size: 0.74rem; }
.job-filter:focus { outline: none; border-color: #6f643e; }
.job-auto { display: flex; align-items: center; gap: 0.4rem; color: var(--muted); font-size: 0.72rem; }

.job-table { border: 1px solid var(--line); border-radius: var(--radius); overflow: hidden; }
.job-row {
  display: grid;
  grid-template-columns: 6.5rem minmax(9rem, 1.4fr) 4.5rem 4rem 5.5rem 5.5rem minmax(7rem, 2fr);
  gap: 1rem; align-items: center; width: 100%;
  padding: 0.6rem 1rem; font-size: 0.72rem; text-align: left;
}
.job-row--head { background: var(--panel-2); color: var(--muted-2); font-family: var(--font-mono); font-size: 0.6rem; text-transform: uppercase; letter-spacing: 0.08em; }
.job-row--click { border: 0; border-top: 1px solid var(--line-soft); background: none; color: inherit; cursor: pointer; }
.job-item:first-of-type .job-row--click { border-top: 0; }
.job-row--click:hover { background: rgba(255, 255, 255, 0.015); }
.job-item.is-open .job-row--click { background: rgba(241, 201, 107, 0.05); }
.job-kind { overflow: hidden; color: var(--text-dim); text-overflow: ellipsis; white-space: nowrap; }
.job-id, .job-attempt, .job-when, .job-running { color: var(--muted-2); font-family: var(--font-mono); font-size: 0.66rem; }
.job-attempt.is-exhausted, .job-running.is-stuck { color: var(--danger); }
.job-error { overflow: hidden; color: #b58a84; font-family: var(--font-mono); font-size: 0.64rem; text-overflow: ellipsis; white-space: nowrap; }

.job-detail { padding: 0.5rem 1rem 1.1rem; border-top: 1px solid var(--line-soft); background: var(--panel-2); }
.job-detail__meta { display: flex; flex-wrap: wrap; gap: 0.4rem 1.25rem; margin-bottom: 0.7rem; color: var(--muted); font-size: 0.68rem; }
.job-detail__meta b { color: var(--muted-2); font-weight: 400; text-transform: uppercase; font-size: 0.58rem; letter-spacing: 0.06em; }
.job-detail__error { margin-bottom: 0.7rem; padding: 0.6rem 0.8rem; border-radius: var(--radius-sm); background: rgba(227, 113, 99, 0.08); color: #d99b93; font-family: var(--font-mono); font-size: 0.66rem; white-space: pre-wrap; word-break: break-word; }
.job-detail__args pre { overflow: auto; max-height: 22rem; margin: 0.4rem 0 0; padding: 0.9rem 1rem; border: 1px solid var(--line); border-radius: var(--radius-sm); background: #0a0d0f; }
.job-detail__args code { color: #aabdb5; font: 0.66rem / 1.6 var(--font-mono); }

.badge { display: inline-block; padding: 0.15rem 0.5rem; border: 1px solid color-mix(in srgb, var(--tone, var(--muted-2)) 45%, transparent); border-radius: 2rem; color: var(--tone, var(--muted-2)); font-size: 0.6rem; text-transform: capitalize; }

@media (max-width: 900px) {
  .job-row { grid-template-columns: 5.5rem 1fr 4rem 5rem; }
  .job-row span:nth-child(3), .job-row span:nth-child(6), .job-row span:nth-child(7) { display: none; }
}
</style>
