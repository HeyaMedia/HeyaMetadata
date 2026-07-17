import type { NuxtApp } from '#app'

// Session-lifetime reuse for useAsyncData: hand back whatever this session
// already fetched under `key` instead of refetching on every component
// remount, so back/forward navigation renders instantly. The server's
// Cache-Control/ETag contract still governs real freshness — a hard reload
// starts clean and explicit refresh() calls bypass this entirely.
export function sessionCached<T>(key: string, nuxtApp: NuxtApp): T | undefined {
  return nuxtApp.payload.data[key] as T | undefined
}
