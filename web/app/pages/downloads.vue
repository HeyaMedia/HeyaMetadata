<script setup lang="ts">
// HeyaMediaServer ships as a container — no native binaries. The user picks an
// accelerator flavor (CPU / CUDA / OpenVINO) and a variant (all-in-one with
// Postgres bundled, or standard with an external Postgres); the image tag and
// the quick-start command update to match. Confirmed tags: :latest, :latest-aio.
//
// Version + changelog are pulled live from GitHub. Formatted release notes are
// used when the repo has Releases; until then it falls back to the version tags
// so the page is honest about what exists today and upgrades itself for free.
useSeoMeta({
  title: 'Downloads',
  description: 'Pull the HeyaMediaServer container, choose a CPU, CUDA, or OpenVINO flavor, and start streaming in about a minute.',
  twitterCard: 'summary_large_image',
})

const REGISTRY = 'ghcr.io/heyamedia/heya'
const REPO = 'HeyaMedia/Heya'
const REPO_URL = 'https://github.com/HeyaMedia/Heya'
const PACKAGES_URL = 'https://github.com/HeyaMedia/Heya/pkgs/container/heya'

type Flavor = 'cpu' | 'cuda' | 'openvino'
type Variant = 'aio' | 'standard'

interface FlavorMeta { id: Flavor; name: string; sub: string; arch: string; suffix: string; icon: 'cpu' | 'gpu' }
const FLAVORS: FlavorMeta[] = [
  { id: 'cpu', name: 'CPU', sub: 'Software transcoding, runs anywhere.', arch: 'arm64 · amd64', suffix: '', icon: 'cpu' },
  { id: 'cuda', name: 'NVIDIA CUDA', sub: 'NVENC / NVDEC hardware transcoding.', arch: 'NVIDIA GPU', suffix: '-cuda', icon: 'gpu' },
  { id: 'openvino', name: 'Intel OpenVINO', sub: 'Quick Sync / iGPU hardware transcoding.', arch: 'Intel iGPU', suffix: '-openvino', icon: 'gpu' },
]

const flavor = ref<Flavor>('cpu')
const variant = ref<Variant>('aio')

const flavorMeta = computed(() => FLAVORS.find(f => f.id === flavor.value)!)
function tagFor(f: FlavorMeta) {
  return `${REGISTRY}:latest${f.suffix}${variant.value === 'aio' ? '-aio' : ''}`
}
const selectedTag = computed(() => tagFor(flavorMeta.value))

const runCommand = computed(() => {
  const lines = ['docker run -d --name heya -p 8080:8080']
  if (flavor.value === 'cuda') lines.push('--gpus all')
  if (flavor.value === 'openvino') lines.push('--device /dev/dri:/dev/dri')
  if (variant.value === 'aio') {
    lines.push('-v "$HOME/heya-data:/data"', '-v "/path/to/media:/media:ro"',
      '-e HEYA_ADMIN_USERNAME=admin', '-e HEYA_ADMIN_PASSWORD=changeme')
  } else {
    lines.push('-v "$PWD/data:/data"', '-v "/path/to/media:/media:ro"',
      "-e HEYA_DATABASE_URL='postgres://heya:heya@db:5432/heya?sslmode=disable'")
  }
  lines.push(selectedTag.value)
  return lines.join(' \\\n  ')
})

const copied = ref('')
let copyTimer: ReturnType<typeof setTimeout> | undefined
async function copy(text: string, id: string) {
  try {
    await navigator.clipboard.writeText(text)
    copied.value = id
    clearTimeout(copyTimer)
    copyTimer = setTimeout(() => { if (copied.value === id) copied.value = '' }, 1600)
  } catch { /* clipboard unavailable — user can select manually */ }
}
onBeforeUnmount(() => clearTimeout(copyTimer))

// ---- Version + changelog from GitHub -------------------------------------
interface ChangelogEntry { tag: string; title: string; date: string; html: string; url: string; prerelease: boolean }

function semverWeight(name: string): number {
  const m = String(name).match(/(\d+)\.(\d+)\.(\d+)/)
  return m ? Number(m[1]) * 1e6 + Number(m[2]) * 1e3 + Number(m[3]) : 0
}

// Minimal, XSS-safe Markdown → HTML for release bodies: escape first, then only
// re-introduce a controlled tag set with validated (http/https) links.
function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}
function inlineMd(s: string): string {
  return s
    .replace(/`([^`]+)`/g, '<code>$1</code>')
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, '<a href="$2" target="_blank" rel="noopener noreferrer">$1</a>')
}
function renderMd(md: string): string {
  const lines = escapeHtml(md.replace(/\r\n/g, '\n')).split('\n')
  const out: string[] = []
  let inList = false
  const closeList = () => { if (inList) { out.push('</ul>'); inList = false } }
  for (const raw of lines) {
    const line = raw.trim()
    if (!line) { closeList(); continue }
    let m: RegExpMatchArray | null
    if ((m = line.match(/^#{1,2}\s+(.*)$/))) { closeList(); out.push(`<h4>${inlineMd(m[1])}</h4>`); continue }
    if ((m = line.match(/^#{3,6}\s+(.*)$/))) { closeList(); out.push(`<h5>${inlineMd(m[1])}</h5>`); continue }
    if ((m = line.match(/^[-*]\s+(.*)$/))) { if (!inList) { out.push('<ul>'); inList = true } out.push(`<li>${inlineMd(m[1])}</li>`); continue }
    closeList()
    out.push(`<p>${inlineMd(line)}</p>`)
  }
  closeList()
  return out.join('')
}

// Non-blocking: page renders immediately, this fills in when GitHub responds.
const { data: gh, pending: ghPending } = useAsyncData('heya-github', async () => {
  const headers = { Accept: 'application/vnd.github+json' }
  const releases = await $fetch<any[]>(`https://api.github.com/repos/${REPO}/releases?per_page=20`, { headers })
    .catch(() => [])
  const published = (Array.isArray(releases) ? releases : []).filter(r => r && !r.draft)
  if (published.length) {
    const entries: ChangelogEntry[] = published.map(r => ({
      tag: r.tag_name,
      title: r.name && r.name !== r.tag_name ? r.name : '',
      date: r.published_at ?? r.created_at ?? '',
      html: r.body ? renderMd(r.body) : '',
      url: r.html_url,
      prerelease: !!r.prerelease,
    }))
    return { mode: 'releases' as const, version: entries[0]?.tag ?? '', entries }
  }
  const tags = await $fetch<any[]>(`https://api.github.com/repos/${REPO}/tags?per_page=30`, { headers })
    .catch(() => [])
  const sorted = (Array.isArray(tags) ? tags : [])
    .map(t => t?.name)
    .filter(Boolean)
    .sort((a, b) => semverWeight(b) - semverWeight(a))
  return { mode: 'tags' as const, version: sorted[0] ?? '', tags: sorted }
}, { default: () => ({ mode: 'none' as const, version: '' }) })

const version = computed(() => gh.value?.version ?? '')
const releaseEntries = computed<ChangelogEntry[]>(() => (gh.value?.mode === 'releases' ? gh.value.entries : []))
const tagList = computed<string[]>(() => (gh.value?.mode === 'tags' ? gh.value.tags : []))

function fmtDate(iso: string) {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}
</script>

<template>
  <div class="shell page downloads">
    <header class="page-heading">
      <div>
        <span class="section-label">HeyaMediaServer</span>
        <h1 class="editorial">Pull it. Run it. <em>Stream it.</em></h1>
        <p>Heya is a single Go binary with the web UI baked in and Postgres as its only datastore — so it ships as one container. Pick a flavor and you're running in a minute.</p>
      </div>
      <a
        :href="`${REPO_URL}/releases`"
        target="_blank"
        rel="noopener noreferrer"
        class="downloads__version"
        :class="{ 'is-empty': !version }"
      >
        <span>{{ version ? 'Latest release' : 'Releases' }}</span>
        <strong>{{ version || 'GitHub ↗' }}</strong>
      </a>
    </header>

    <!-- Variant: all-in-one (Postgres bundled) vs standard (external Postgres) -->
    <div class="variant">
      <div class="variant__toggle" role="tablist" aria-label="Image variant">
        <button
          type="button" role="tab" :aria-selected="variant === 'aio'"
          class="variant__opt" :class="{ 'is-active': variant === 'aio' }"
          @click="variant = 'aio'"
        >
          All-in-one
          <span>Postgres bundled</span>
        </button>
        <button
          type="button" role="tab" :aria-selected="variant === 'standard'"
          class="variant__opt" :class="{ 'is-active': variant === 'standard' }"
          @click="variant = 'standard'"
        >
          Standard
          <span>Bring your own Postgres</span>
        </button>
      </div>
      <p class="variant__hint">
        {{ variant === 'aio'
          ? 'Everything in one container, including the database — the fastest way to try Heya.'
          : 'Runs against an external Postgres you manage — best for production and existing databases.' }}
      </p>
    </div>

    <!-- Accelerator flavor -->
    <section class="flavors">
      <button
        v-for="f in FLAVORS"
        :key="f.id"
        type="button"
        class="flavor"
        :class="{ 'is-active': flavor === f.id }"
        :aria-pressed="flavor === f.id"
        @click="flavor = f.id"
      >
        <span class="flavor__mark" aria-hidden="true">
          <svg v-if="f.icon === 'cpu'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" width="20" height="20">
            <rect x="6.5" y="6.5" width="11" height="11" rx="1.5" />
            <path d="M9.5 3v2.5M14.5 3v2.5M9.5 18.5V21M14.5 18.5V21M3 9.5h2.5M3 14.5h2.5M18.5 9.5H21M18.5 14.5H21" />
          </svg>
          <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" width="20" height="20">
            <rect x="2.5" y="7" width="17" height="10" rx="2" />
            <path d="M5.5 17v3M9.5 17v3" />
            <circle cx="9" cy="12" r="2.2" />
            <path d="M14.5 10.5h3M14.5 13.5h3" />
          </svg>
        </span>
        <span class="flavor__body">
          <span class="flavor__name">{{ f.name }}<span class="flavor__arch">{{ f.arch }}</span></span>
          <span class="flavor__sub">{{ f.sub }}</span>
        </span>
        <span class="flavor__check" aria-hidden="true">{{ flavor === f.id ? '●' : '○' }}</span>
      </button>
    </section>

    <!-- Selected image + quick start -->
    <section class="run">
      <div class="run__tag">
        <div>
          <span class="section-label">Image</span>
          <code>{{ selectedTag }}</code>
        </div>
        <button type="button" class="btn btn--sm" @click="copy(selectedTag, 'tag')">{{ copied === 'tag' ? 'Copied' : 'Copy tag' }}</button>
      </div>

      <div class="run__cmd">
        <header class="run__cmd-head">
          <span class="section-label">Quick start</span>
          <button type="button" class="btn btn--sm btn--gold" @click="copy(runCommand, 'run')">{{ copied === 'run' ? 'Copied' : 'Copy command' }}</button>
        </header>
        <pre><code>{{ runCommand }}</code></pre>
        <p class="run__cmd-note">Then open <code>http://localhost:8080</code>. Swap <code>/path/to/media</code> for your library.</p>
      </div>
    </section>

    <!-- Reference -->
    <section class="ref">
      <dl class="ref__facts">
        <div><dt>Port</dt><dd>8080</dd></div>
        <div><dt>State &amp; config</dt><dd><code>/data</code></dd></div>
        <div><dt>Media library</dt><dd><code>/media</code> (read-only)</dd></div>
        <div><dt>Registry</dt><dd>{{ REGISTRY }}</dd></div>
      </dl>
      <div class="ref__links">
        <a :href="REPO_URL" target="_blank" rel="noopener noreferrer" class="ref__link">Source on GitHub ↗</a>
        <a :href="PACKAGES_URL" target="_blank" rel="noopener noreferrer" class="ref__link">All image tags ↗</a>
        <NuxtLink to="/features" class="ref__link">What Heya does ↗</NuxtLink>
      </div>
    </section>

    <!-- Changelog -->
    <section class="changelog">
      <header class="section-head">
        <div>
          <span class="section-label">Changelog</span>
          <h2>Release history</h2>
        </div>
        <a :href="`${REPO_URL}/releases`" target="_blank" rel="noopener noreferrer" class="changelog__all">All on GitHub ↗</a>
      </header>

      <p v-if="ghPending" class="muted changelog__loading">Loading release history…</p>

      <!-- Rich release notes -->
      <div v-else-if="releaseEntries.length" class="changelog__list">
        <article v-for="entry in releaseEntries" :key="entry.tag" class="cl">
          <div class="cl__aside">
            <a :href="entry.url" target="_blank" rel="noopener noreferrer" class="cl__tag">{{ entry.tag }}</a>
            <span v-if="entry.prerelease" class="cl__pre">pre-release</span>
            <span v-if="fmtDate(entry.date)" class="cl__date">{{ fmtDate(entry.date) }}</span>
          </div>
          <div class="cl__body">
            <h3 v-if="entry.title" class="cl__title">{{ entry.title }}</h3>
            <!-- eslint-disable-next-line vue/no-v-html — sanitized by renderMd() -->
            <div v-if="entry.html" class="cl__notes" v-html="entry.html" />
            <p v-else class="cl__empty">No notes for this release yet.</p>
          </div>
        </article>
      </div>

      <!-- Tags only (no Releases published yet) -->
      <div v-else-if="tagList.length" class="changelog__tags-wrap">
        <p class="changelog__note">Every version is tagged on GitHub. Formatted release notes are on the way — until then, browse a tag for its diff.</p>
        <ul class="changelog__tags">
          <li v-for="tag in tagList" :key="tag">
            <a :href="`${REPO_URL}/releases/tag/${tag}`" target="_blank" rel="noopener noreferrer">{{ tag }}</a>
          </li>
        </ul>
      </div>

      <p v-else class="muted changelog__loading">Release history is unavailable right now. <a :href="`${REPO_URL}/releases`" target="_blank" rel="noopener noreferrer" class="ref__link">View it on GitHub ↗</a></p>
    </section>
  </div>
</template>

<style scoped>
.downloads__version {
  display: flex;
  flex-direction: column;
  gap: 0.15rem;
  align-self: flex-end;
  padding: 0.4rem 0.7rem;
  border: 1px solid var(--line-strong);
  border-radius: var(--radius-sm);
  text-align: right;
  transition: border-color 0.15s ease;
}
.downloads__version:hover { border-color: var(--gold); }
.downloads__version span { color: var(--muted-2); font-size: 0.58rem; letter-spacing: 0.06em; text-transform: uppercase; }
.downloads__version strong { color: var(--gold); font-family: var(--font-mono); font-size: 0.9rem; font-weight: 500; }

/* Variant toggle */
.variant { margin-bottom: 1.75rem; }
.variant__toggle {
  display: inline-flex;
  gap: 0.25rem;
  padding: 0.25rem;
  border: 1px solid var(--line);
  border-radius: var(--radius);
  background: var(--panel-2);
}
.variant__opt {
  display: flex;
  flex-direction: column;
  gap: 0.15rem;
  padding: 0.55rem 1.1rem;
  border: 0;
  border-radius: var(--radius-sm);
  background: none;
  color: var(--muted);
  font-size: 0.82rem;
  text-align: left;
  transition: background 0.15s ease, color 0.15s ease;
}
.variant__opt span { color: var(--muted-2); font-size: 0.62rem; }
.variant__opt.is-active { background: var(--panel-raised); color: var(--text); }
.variant__opt.is-active span { color: var(--gold); }
.variant__hint { margin: 0.75rem 0 0; color: var(--muted); font-size: 0.76rem; }

/* Flavor cards */
.flavors {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 0.85rem;
  margin-bottom: 1.5rem;
}
.flavor {
  display: flex;
  align-items: flex-start;
  gap: 0.85rem;
  padding: 1.15rem;
  border: 1px solid var(--line);
  border-radius: var(--radius);
  background: var(--panel);
  text-align: left;
  transition: border-color 0.15s ease, background 0.15s ease;
}
.flavor:hover { border-color: var(--line-strong); }
.flavor.is-active { border-color: #6f643e; background: linear-gradient(180deg, rgba(241, 201, 107, 0.05), var(--panel)); }
.flavor__mark {
  display: grid;
  place-items: center;
  flex: none;
  width: 2.4rem;
  height: 2.4rem;
  border: 1px solid var(--line-strong);
  border-radius: 50%;
  color: var(--gold);
}
.flavor__body { min-width: 0; flex: 1 1 auto; }
.flavor__name { display: flex; align-items: baseline; gap: 0.5rem; color: var(--text); font-size: 0.92rem; font-weight: 500; }
.flavor__arch { color: var(--muted-2); font-family: var(--font-mono); font-size: 0.58rem; letter-spacing: 0.04em; }
.flavor__sub { display: block; margin-top: 0.3rem; color: var(--muted); font-size: 0.72rem; line-height: 1.5; }
.flavor__check { flex: none; color: var(--muted-2); font-size: 0.7rem; }
.flavor.is-active .flavor__check { color: var(--gold); }

/* Image tag + quick start */
.run { display: flex; flex-direction: column; gap: 1rem; }
.run__tag {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.9rem 1rem 0.9rem 1.15rem;
  border: 1px solid var(--line);
  border-radius: var(--radius);
  background: var(--panel);
}
.run__tag .section-label { display: block; margin-bottom: 0.35rem; }
.run__tag code { color: var(--text-dim); font-family: var(--font-mono); font-size: 0.82rem; word-break: break-all; }

.run__cmd { border: 1px solid var(--line); border-radius: var(--radius); background: var(--bg); overflow: hidden; }
.run__cmd-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.85rem 1rem;
  border-bottom: 1px solid var(--line);
  background: var(--panel-2);
}
.run__cmd pre { margin: 0; padding: 1.1rem 1.25rem; overflow-x: auto; }
.run__cmd code { color: var(--text-dim); font-family: var(--font-mono); font-size: 0.76rem; line-height: 1.75; white-space: pre; }
.run__cmd-note { margin: 0; padding: 0 1.25rem 1.1rem; color: var(--muted-2); font-size: 0.72rem; }
.run__cmd-note code { padding: 0.05rem 0.3rem; border-radius: 3px; background: var(--panel); color: var(--text-dim); font-size: 0.7rem; }

/* Reference */
.ref {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 1.5rem;
  margin-top: 2rem;
  padding-top: 1.5rem;
  border-top: 1px solid var(--line);
}
.ref__facts { display: flex; flex-wrap: wrap; gap: 1.75rem; margin: 0; }
.ref__facts div { display: flex; flex-direction: column-reverse; gap: 0.25rem; }
.ref__facts dt { color: var(--muted-2); font-size: 0.62rem; }
.ref__facts dd { margin: 0; color: var(--text-dim); font-size: 0.82rem; }
.ref__facts dd code { font-family: var(--font-mono); font-size: 0.78rem; }
.ref__links { display: flex; flex-wrap: wrap; gap: 1.25rem; }
.ref__link { color: var(--muted); font-size: 0.76rem; }
.ref__link:hover { color: var(--gold); }

/* Changelog */
.changelog { margin-top: 3rem; }
.changelog__all { color: var(--muted); font-size: 0.74rem; }
.changelog__all:hover { color: var(--gold); }
.changelog__loading { margin: 1.5rem 0 0; font-size: 0.82rem; }

.changelog__list { display: flex; flex-direction: column; }
.cl {
  display: grid;
  grid-template-columns: 12rem 1fr;
  gap: 1.5rem;
  padding: 1.6rem 0;
  border-top: 1px solid var(--line-soft);
}
.cl:first-child { border-top: 0; }
.cl__aside { display: flex; flex-direction: column; align-items: flex-start; gap: 0.4rem; }
.cl__tag {
  padding: 0.25rem 0.55rem;
  border: 1px solid #6f643e;
  border-radius: var(--radius-sm);
  color: var(--gold);
  font-family: var(--font-mono);
  font-size: 0.78rem;
}
.cl__tag:hover { background: rgba(241, 201, 107, 0.08); }
.cl__pre { color: var(--danger); font-family: var(--font-mono); font-size: 0.6rem; text-transform: uppercase; letter-spacing: 0.05em; }
.cl__date { color: var(--muted-2); font-size: 0.68rem; }
.cl__title { margin: 0 0 0.6rem; font-size: 1.02rem; font-weight: 500; }
.cl__empty { margin: 0; color: var(--muted-2); font-size: 0.78rem; font-style: italic; }

/* Rendered release-note markdown */
.cl__notes { color: var(--text-dim); font-size: 0.82rem; line-height: 1.7; }
.cl__notes :deep(h4) { margin: 1.1rem 0 0.4rem; font-size: 0.9rem; font-weight: 600; color: var(--text); }
.cl__notes :deep(h5) { margin: 1rem 0 0.35rem; font-size: 0.78rem; font-weight: 600; color: var(--muted); text-transform: uppercase; letter-spacing: 0.04em; }
.cl__notes :deep(h4:first-child), .cl__notes :deep(h5:first-child) { margin-top: 0; }
.cl__notes :deep(p) { margin: 0.5rem 0; }
.cl__notes :deep(ul) { margin: 0.5rem 0; padding-left: 1.1rem; }
.cl__notes :deep(li) { margin: 0.25rem 0; }
.cl__notes :deep(a) { color: var(--gold); }
.cl__notes :deep(a:hover) { text-decoration: underline; }
.cl__notes :deep(code) {
  padding: 0.05rem 0.3rem;
  border-radius: 3px;
  background: var(--panel);
  font-family: var(--font-mono);
  font-size: 0.76rem;
}

/* Tags-only fallback */
.changelog__tags-wrap { margin-top: 1.25rem; }
.changelog__note { margin: 0 0 1rem; color: var(--muted); font-size: 0.8rem; max-width: 44rem; line-height: 1.6; }
.changelog__tags { display: flex; flex-wrap: wrap; gap: 0.5rem; margin: 0; padding: 0; list-style: none; }
.changelog__tags a {
  display: inline-block;
  padding: 0.35rem 0.6rem;
  border: 1px solid var(--line-strong);
  border-radius: var(--radius-sm);
  color: var(--text-dim);
  font-family: var(--font-mono);
  font-size: 0.74rem;
}
.changelog__tags a:hover { border-color: var(--gold); color: var(--gold); }

@media (max-width: 640px) {
  .cl { grid-template-columns: 1fr; gap: 0.75rem; }
  .cl__aside { flex-direction: row; align-items: center; gap: 0.75rem; }
}
@media (max-width: 560px) {
  .variant__toggle { width: 100%; }
  .variant__opt { flex: 1 1 0; }
}
</style>
