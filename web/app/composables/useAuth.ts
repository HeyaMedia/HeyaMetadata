import { computed, ref } from 'vue'
import type { ApiKey, AuthUser } from '../utils/types'

// Session auth against the same-origin backend. The session lives in an
// httpOnly, SameSite=Strict cookie set by the server — the browser sends it
// automatically on same-origin requests, and JS can neither read nor store it.
// We only ever hold the non-sensitive user object (id/username/role); the
// password exists only in the login/register request body and is never stored.

const BASE = '/api/v2/auth'

// Module-level singleton so every component shares one auth state.
const user = ref<AuthUser | null>(null)
const ready = ref(false)
let hydration: Promise<void> | null = null

function messageFrom(body: any, response: Response): string {
  if (body && typeof body === 'object') return body.detail || body.title || body.error || `${response.status} ${response.statusText}`
  return `${response.status} ${response.statusText}`
}

async function post(path: string, payload?: Record<string, unknown>): Promise<any> {
  const response = await fetch(`${BASE}${path}`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: payload ? { 'Content-Type': 'application/json' } : {},
    body: payload ? JSON.stringify(payload) : undefined,
  })
  const contentType = response.headers.get('content-type') || ''
  const body = response.status === 204 ? null : contentType.includes('json') ? await response.json() : await response.text()
  if (!response.ok) throw new Error(messageFrom(body, response))
  return body
}

export function useAuth() {
  // Hydrate the current user from the session cookie. Tolerates the endpoint
  // being absent (e.g. before the backend ships) — treated as signed out.
  async function fetchMe() {
    try {
      const response = await fetch(`${BASE}/me`, { credentials: 'same-origin' })
      user.value = response.ok ? (await response.json()).user ?? null : null
    } catch {
      user.value = null
    } finally {
      ready.value = true
    }
  }

  // Run the initial /me exactly once per page load.
  function hydrate() {
    if (!hydration) hydration = fetchMe()
    return hydration
  }

  async function login(username: string, password: string, altcha?: string | null) {
    const data = await post('/login', { username, password, ...(altcha ? { altcha } : {}) })
    user.value = data?.user ?? null
    return user.value
  }

  async function register(username: string, password: string, altcha?: string | null) {
    const data = await post('/register', { username, password, ...(altcha ? { altcha } : {}) })
    user.value = data?.user ?? null
    return user.value
  }

  async function logout() {
    try {
      await post('/logout')
    } finally {
      user.value = null
    }
  }

  // ---- Heya-issued API keys (Bearer access to the user's own account) -------

  function listApiKeys(): Promise<ApiKey[]> {
    return fetch(`${BASE}/api-keys`, { credentials: 'same-origin' })
      .then(async r => {
        if (!r.ok) throw new Error(`${r.status} ${r.statusText}`)
        return (await r.json()).api_keys ?? []
      })
  }

  async function createApiKey(name: string): Promise<ApiKey> {
    const data = await post('/api-keys', { name })
    return data?.api_key
  }

  async function revokeApiKey(id: string): Promise<void> {
    await fetch(`${BASE}/api-keys/${encodeURIComponent(id)}`, { method: 'DELETE', credentials: 'same-origin' })
      .then(r => { if (!r.ok && r.status !== 204) throw new Error(`${r.status} ${r.statusText}`) })
  }

  return {
    user,
    ready,
    isAuthenticated: computed(() => !!user.value),
    isAdmin: computed(() => user.value?.role === 'admin'),
    hydrate,
    fetchMe,
    login,
    register,
    logout,
    listApiKeys,
    createApiKey,
    revokeApiKey,
  }
}
