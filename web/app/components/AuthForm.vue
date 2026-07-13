<script setup lang="ts">
// Login / register form. The password is bound to a local ref, sent once in the
// request body, and never stored anywhere else. On success the server sets the
// session cookie and we return to `?redirect=` (or home).
const props = defineProps<{ mode: 'login' | 'register' }>()

const route = useRoute()
const auth = useAuth()

const username = ref('')
const password = ref('')
const confirm = ref('')
const error = ref('')
const loading = ref(false)

const isRegister = computed(() => props.mode === 'register')
const redirect = computed(() => (typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/') ? route.query.redirect : '/'))

const clientError = computed(() => {
  if (!username.value.trim() || !password.value) return ''
  if (isRegister.value && password.value.length < 10) return 'Password must be at least 10 characters.'
  if (isRegister.value && confirm.value !== password.value) return 'Passwords do not match.'
  return ''
})
const canSubmit = computed(() => !!username.value.trim() && !!password.value && !clientError.value && !loading.value)

async function submit() {
  if (!canSubmit.value) {
    error.value = clientError.value
    return
  }
  loading.value = true
  error.value = ''
  try {
    if (isRegister.value) await auth.register(username.value.trim(), password.value)
    else await auth.login(username.value.trim(), password.value)
    await navigateTo(redirect.value)
  } catch (reason: any) {
    error.value = reason?.message || 'Something went wrong. Try again.'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="shell page auth">
    <div class="auth__card">
      <span class="section-label">{{ isRegister ? 'Join the library' : 'Welcome back' }}</span>
      <h1 class="editorial auth__title">{{ isRegister ? 'Create account' : 'Sign in' }}</h1>
      <p class="auth__intro">
        {{ isRegister
          ? 'An account is just an identifier for anything you contribute — no email required.'
          : 'Sign in to your Heya account.' }}
      </p>

      <form class="auth__form" @submit.prevent="submit">
        <label class="auth__field">
          <span>Username</span>
          <input
            v-model="username"
            type="text"
            name="username"
            autocomplete="username"
            autocapitalize="none"
            spellcheck="false"
            required
            maxlength="64"
          >
        </label>

        <label class="auth__field">
          <span>Password</span>
          <input
            v-model="password"
            type="password"
            name="password"
            :autocomplete="isRegister ? 'new-password' : 'current-password'"
            required
            minlength="8"
          >
        </label>

        <label v-if="isRegister" class="auth__field">
          <span>Confirm password</span>
          <input v-model="confirm" type="password" name="confirm" autocomplete="new-password" required>
        </label>

        <p v-if="isRegister" class="auth__hint">At least 10 characters. Choose something you don't use elsewhere.</p>

        <div v-if="error || clientError" class="notice auth__error">
          <span>{{ error || clientError }}</span>
        </div>

        <button type="submit" class="btn btn--gold auth__submit" :disabled="!canSubmit">
          {{ loading ? 'Please wait…' : isRegister ? 'Create account' : 'Sign in' }}
        </button>
      </form>

      <p class="auth__switch">
        <template v-if="isRegister">
          Already have an account? <NuxtLink :to="{ path: '/login', query: route.query }">Sign in</NuxtLink>
        </template>
        <template v-else>
          Don't have an account? <NuxtLink :to="{ path: '/register', query: route.query }">Create one</NuxtLink>
        </template>
      </p>
    </div>
  </div>
</template>

<style scoped>
.auth { display: grid; place-items: center; min-height: calc(100vh - 16rem); }
.auth__card {
  width: 100%;
  max-width: 26rem;
  padding: clamp(1.5rem, 4vw, 2.5rem);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  background: rgba(17, 22, 25, 0.7);
}
.auth__title { margin: 0.7rem 0 0.5rem; font-size: clamp(1.9rem, 4vw, 2.6rem); }
.auth__intro { margin: 0 0 1.5rem; color: var(--muted); font-size: 0.82rem; line-height: 1.6; }
.auth__form { display: flex; flex-direction: column; gap: 0.9rem; }
.auth__field { display: flex; flex-direction: column; gap: 0.4rem; }
.auth__field span { color: #788388; font-size: 0.62rem; letter-spacing: 0.08em; text-transform: uppercase; }
.auth__field input {
  padding: 0.7rem 0.8rem;
  border: 1px solid var(--line-strong);
  border-radius: var(--radius-sm);
  outline: none;
  background: var(--panel-2);
  color: var(--text);
  font-size: 0.85rem;
}
.auth__field input:focus { border-color: #6f643e; }
.auth__hint { margin: -0.2rem 0 0; color: var(--muted-2); font-size: 0.68rem; }
.auth__error { margin: 0.2rem 0 0; }
.auth__submit { margin-top: 0.4rem; padding-block: 0.8rem; }
.auth__switch { margin: 1.4rem 0 0; color: var(--muted); font-size: 0.78rem; text-align: center; }
.auth__switch a { color: var(--gold); }
.auth__switch a:hover { text-decoration: underline; }
</style>
