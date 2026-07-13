<script setup lang="ts">
import { KINDS } from '~/utils/kinds'

// Sticky, shell-aligned header. Desktop shows brand + primary nav + compact
// search + settings + health. Mobile collapses nav, search, settings, health,
// and domain shortcuts into a menu without hiding the primary routes.
const route = useRoute()
const api = useHeyaApi()
const { activeCount } = useProviderCredentials()

const panel = ref<'' | 'keys' | 'menu'>('')
const health = ref<boolean | null>(null)

const primaryNav = [
  { label: 'Latest', to: '/', exact: true },
  { label: 'Browse', to: '/browse', exact: false },
  { label: 'Collections', to: '/collections', exact: false },
  { label: 'Stats', to: '/stats', exact: false },
]

const domainNav = KINDS.filter(kind => kind.route && !kind.route.includes('/'))
  .map(kind => ({ label: kind.plural, to: `/${kind.route}` }))
  // de-duplicate plural labels that share a base route
  .filter((item, index, all) => all.findIndex(other => other.to === item.to) === index)

function isActive(to: string, exact: boolean) {
  return exact ? route.path === to : route.path === to || route.path.startsWith(`${to}/`)
}

function toggle(target: 'keys' | 'menu') {
  panel.value = panel.value === target ? '' : target
}

// Close overlays on navigation.
watch(() => route.fullPath, () => { panel.value = '' })

onMounted(async () => {
  try {
    const result = await api.health()
    health.value = result.status === 'ok'
  } catch {
    health.value = false
  }
})

const healthLabel = computed(() =>
  health.value === null ? 'Checking API' : health.value ? 'Systems ready' : 'API unavailable',
)
</script>

<template>
  <header class="app-header">
    <div class="shell app-header__inner">
      <NuxtLink to="/" class="brand" aria-label="Heya Metadata home">
        <span class="brand__mark">H</span>
        <span class="brand__text">
          <strong>Heya</strong>
          <small>Metadata Observatory</small>
        </span>
      </NuxtLink>

      <nav class="app-header__nav" aria-label="Primary">
        <NuxtLink
          v-for="item in primaryNav"
          :key="item.to"
          :to="item.to"
          :class="{ 'is-active': isActive(item.to, item.exact) }"
        >{{ item.label }}</NuxtLink>
      </nav>

      <GlobalSearch class="app-header__search" size="bar" :initial-query="(route.query.q as string) || ''" />

      <div class="app-header__tools">
        <button
          type="button"
          class="tool-button"
          :class="{ 'is-open': panel === 'keys' }"
          :aria-expanded="panel === 'keys'"
          @click="toggle('keys')"
        >
          Provider keys
          <span v-if="activeCount" class="tool-button__badge">{{ activeCount }}</span>
        </button>

        <span class="health" :class="{ 'is-healthy': health, 'is-down': health === false }" :title="healthLabel">
          <i aria-hidden="true" />
          <span class="health__text">{{ healthLabel }}</span>
        </span>

        <button
          type="button"
          class="app-header__burger"
          :aria-expanded="panel === 'menu'"
          aria-label="Open menu"
          @click="toggle('menu')"
        >
          <span /><span /><span />
        </button>
      </div>
    </div>

    <Transition name="drawer">
      <ProviderKeysPanel v-if="panel === 'keys'" />
    </Transition>

    <Transition name="drawer">
      <nav v-if="panel === 'menu'" class="mobile-menu" aria-label="Mobile">
        <div class="shell mobile-menu__inner">
          <GlobalSearch size="bar" :initial-query="(route.query.q as string) || ''" />
          <div class="mobile-menu__group">
            <span class="section-label">Library</span>
            <NuxtLink v-for="item in primaryNav" :key="item.to" :to="item.to">{{ item.label }}</NuxtLink>
          </div>
          <div class="mobile-menu__group">
            <span class="section-label">Domains</span>
            <NuxtLink v-for="item in domainNav" :key="item.to" :to="item.to">{{ item.label }}</NuxtLink>
          </div>
          <div class="mobile-menu__group">
            <button type="button" class="btn btn--ghost" @click="panel = 'keys'">
              Provider keys<span v-if="activeCount"> · {{ activeCount }} set</span>
            </button>
            <span class="health health--inline" :class="{ 'is-healthy': health, 'is-down': health === false }">
              <i aria-hidden="true" />{{ healthLabel }}
            </span>
          </div>
        </div>
      </nav>
    </Transition>
  </header>
</template>

<style scoped>
.app-header {
  position: sticky;
  top: 0;
  z-index: 40;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  background: rgba(11, 14, 16, 0.86);
  backdrop-filter: blur(20px);
}
.app-header__inner {
  display: flex;
  align-items: center;
  gap: 1.5rem;
  height: var(--header-h);
}

.brand { display: flex; gap: 0.7rem; align-items: center; flex: 0 0 auto; }
.brand__mark {
  display: grid;
  width: 2.15rem;
  height: 2.15rem;
  place-items: center;
  border: 1px solid #817247;
  border-radius: 50%;
  color: var(--gold);
  font-family: var(--font-serif);
  font-size: 1.2rem;
  font-style: italic;
}
.brand__text strong, .brand__text small { display: block; }
.brand__text strong { font-size: 0.92rem; letter-spacing: 0.08em; }
.brand__text small { margin-top: 0.1rem; color: #718087; font-size: 0.6rem; letter-spacing: 0.12em; text-transform: uppercase; }

.app-header__nav { display: flex; align-items: center; gap: 1.35rem; font-size: 0.76rem; }
.app-header__nav a { color: #9ba5a9; }
.app-header__nav a:hover { color: #fff; }
.app-header__nav a.is-active { color: var(--gold); }

.app-header__search { flex: 1 1 auto; max-width: 24rem; margin-left: auto; }

.app-header__tools { display: flex; align-items: center; gap: 1rem; flex: 0 0 auto; }
.tool-button {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  border: 0;
  background: none;
  color: #9ba5a9;
  font-size: 0.74rem;
}
.tool-button:hover, .tool-button.is-open { color: #fff; }
.tool-button__badge {
  display: inline-grid;
  width: 1.2rem;
  height: 1.2rem;
  place-items: center;
  border-radius: 50%;
  background: var(--gold);
  color: #18150c;
  font-size: 0.6rem;
  font-weight: 800;
}

.health { display: flex; align-items: center; gap: 0.45rem; color: #6f797d; font-size: 0.72rem; }
.health i { width: 0.42rem; height: 0.42rem; border-radius: 50%; background: #737b7e; }
.health.is-healthy { color: #9cb7a9; }
.health.is-healthy i { background: var(--green); box-shadow: 0 0 0 4px rgba(121, 214, 170, 0.08); }
.health.is-down { color: #d6978f; }
.health.is-down i { background: var(--danger); }

.app-header__burger { display: none; flex-direction: column; gap: 4px; border: 0; background: none; padding: 0.4rem; }
.app-header__burger span { width: 1.3rem; height: 1.5px; background: #b6bfc0; border-radius: 2px; }

.drawer-enter-active, .drawer-leave-active { transition: opacity 0.2s ease, transform 0.2s ease; }
.drawer-enter-from, .drawer-leave-to { opacity: 0; transform: translateY(-0.5rem); }

.mobile-menu { border-bottom: 1px solid var(--line); background: #0f1417; }
.mobile-menu__inner { display: flex; flex-direction: column; gap: 1.25rem; padding-block: 1.25rem 1.5rem; }
.mobile-menu__group { display: flex; flex-wrap: wrap; align-items: center; gap: 0.9rem; }
.mobile-menu__group .section-label { flex-basis: 100%; }
.mobile-menu__group a { color: #c3ccca; font-size: 0.85rem; }
.mobile-menu__group a:hover { color: #fff; }
.health--inline { font-size: 0.75rem; }

@media (max-width: 900px) {
  .app-header__nav { display: none; }
  .app-header__search { display: none; }
  .app-header__burger { display: flex; }
  .health__text { display: none; }
}
</style>
