import { computed, ref } from 'vue'
import type { AuthUser } from '../utils/types'

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

  async function login(username: string, password: string) {
    const data = await post('/login', { username, password })
    user.value = data?.user ?? null
    return user.value
  }

  async function register(username: string, password: string) {
    const data = await post('/register', { username, password })
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

  return {
    user,
    ready,
    isAuthenticated: computed(() => !!user.value),
    hydrate,
    fetchMe,
    login,
    register,
    logout,
  }
}
