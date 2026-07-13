<script setup lang="ts">
import { KINDS } from '~/utils/kinds'

// Sticky, shell-aligned header. Desktop shows brand + primary nav + compact
// search + settings + health. Mobile collapses nav, search, settings, health,
// and domain shortcuts into a menu without hiding the primary routes.
const route = useRoute()
const { activeCount } = useProviderCredentials()
const auth = useAuth()
const { user, ready: authReady, isAuthenticated } = auth

const panel = ref<'' | 'keys' | 'menu' | 'account'>('')

async function signOut() {
  panel.value = ''
  await auth.logout()
  await navigateTo('/')
}

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

function toggle(target: 'keys' | 'menu' | 'account') {
  panel.value = panel.value === target ? '' : target
}

// Close overlays on navigation.
watch(() => route.fullPath, () => { panel.value = '' })

const onAuthPage = computed(() => route.path === '/login' || route.path === '/register')
const loginTarget = computed(() => ({ path: '/login', query: onAuthPage.value ? {} : { redirect: route.fullPath } }))
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
        <div v-if="authReady" class="account">
          <template v-if="isAuthenticated && user">
            <button
              type="button"
              class="account__toggle"
              :class="{ 'is-open': panel === 'account' }"
              :aria-expanded="panel === 'account'"
              @click="toggle('account')"
            >
              <span class="account__avatar">{{ user.username.charAt(0).toUpperCase() }}</span>
              <span class="account__name">{{ user.username }}</span>
            </button>
            <Transition name="pop">
              <div v-if="panel === 'account'" class="account__menu">
                <NuxtLink to="/account" class="account__item">Account</NuxtLink>
                <button type="button" class="account__item" @click="panel = 'keys'">
                  Provider keys<span v-if="activeCount" class="account__badge">{{ activeCount }}</span>
                </button>
                <button type="button" class="account__item" @click="signOut">Sign out</button>
              </div>
            </Transition>
          </template>
          <NuxtLink v-else class="tool-button account__signin" :to="loginTarget">Sign in</NuxtLink>
        </div>

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
      <ProviderKeysPanel v-if="panel === 'keys' && isAuthenticated" />
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
          <div v-if="authReady" class="mobile-menu__group">
            <span class="section-label">Account</span>
            <template v-if="isAuthenticated && user">
              <NuxtLink to="/account">{{ user.username }}</NuxtLink>
              <button type="button" class="btn btn--ghost" @click="panel = 'keys'">
                Provider keys<span v-if="activeCount"> · {{ activeCount }} set</span>
              </button>
              <button type="button" class="btn btn--ghost" @click="signOut">Sign out</button>
            </template>
            <template v-else>
              <NuxtLink :to="loginTarget">Sign in</NuxtLink>
              <NuxtLink to="/register">Create account</NuxtLink>
            </template>
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

.account { position: relative; display: flex; }
.account__toggle { display: inline-flex; align-items: center; gap: 0.5rem; border: 0; background: none; color: #9ba5a9; font-size: 0.74rem; }
.account__toggle:hover, .account__toggle.is-open { color: #fff; }
.account__avatar {
  display: grid;
  place-items: center;
  width: 1.65rem;
  height: 1.65rem;
  border: 1px solid #817247;
  border-radius: 50%;
  color: var(--gold);
  font-size: 0.68rem;
  font-weight: 700;
}
.account__name { max-width: 9rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.account__menu {
  position: absolute;
  top: calc(100% + 0.65rem);
  right: 0;
  z-index: 30;
  display: flex;
  flex-direction: column;
  min-width: 10rem;
  padding: 0.35rem;
  border: 1px solid var(--line);
  border-radius: var(--radius);
  background: #12181c;
  box-shadow: 0 1rem 2.5rem rgba(0, 0, 0, 0.5);
}
.account__item {
  padding: 0.55rem 0.7rem;
  border: 0;
  border-radius: var(--radius-sm);
  background: none;
  color: #cbd1ce;
  font-size: 0.76rem;
  text-align: left;
}
.account__item { display: flex; align-items: center; justify-content: space-between; gap: 0.6rem; }
.account__item:hover { background: rgba(255, 255, 255, 0.04); color: #fff; }
.account__badge {
  display: inline-grid;
  place-items: center;
  min-width: 1.2rem;
  height: 1.2rem;
  padding: 0 0.3rem;
  border-radius: 1rem;
  background: var(--gold);
  color: #18150c;
  font-size: 0.6rem;
  font-weight: 800;
}
.pop-enter-active, .pop-leave-active { transition: opacity 0.15s ease, transform 0.15s ease; }
.pop-enter-from, .pop-leave-to { opacity: 0; transform: translateY(-0.3rem); }

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

@media (max-width: 900px) {
  .app-header__nav { display: none; }
  .app-header__search { display: none; }
  .app-header__burger { display: flex; }
  .account { display: none; }
}
</style>
