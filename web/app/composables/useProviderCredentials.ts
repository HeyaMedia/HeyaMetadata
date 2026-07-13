import { computed, reactive } from 'vue'

// Request-scoped provider credentials. These live ONLY in this module's reactive
// state for the lifetime of the page. They are never written to localStorage,
// sessionStorage, cookies, URLs, or logs, and a reload forgets everything.
// Each field maps to the server's short-lived X-Heya-* credential-vault headers.

export interface CredentialField {
  key: string
  header: string
  label: string
}

export const CREDENTIAL_FIELDS: CredentialField[] = [
  { key: 'tmdb', header: 'X-Heya-TMDB-API-Key', label: 'TMDB API key' },
  { key: 'omdb', header: 'X-Heya-OMDB-API-Key', label: 'OMDB API key' },
  { key: 'tvdb', header: 'X-Heya-TVDB-API-Key', label: 'TVDB API key' },
  { key: 'fanart', header: 'X-Heya-Fanart-API-Key', label: 'Fanart API key' },
  { key: 'apple', header: 'X-Heya-Apple-API-Key', label: 'Apple API key' },
  { key: 'discogs', header: 'X-Heya-Discogs-API-Key', label: 'Discogs token' },
  { key: 'lastfm', header: 'X-Heya-LastFM-API-Key', label: 'Last.fm API key' },
  { key: 'googlebooks', header: 'X-Heya-Google-Books-API-Key', label: 'Google Books API key' },
  { key: 'mal', header: 'X-Heya-MAL-Client-ID', label: 'MyAnimeList client ID' },
]

const credentials = reactive<Record<string, string>>(
  Object.fromEntries(CREDENTIAL_FIELDS.map(field => [field.key, ''])),
)

export function useProviderCredentials() {
  const activeCount = computed(() => Object.values(credentials).filter(value => value.trim()).length)

  function headers(): Record<string, string> {
    const out: Record<string, string> = {}
    for (const field of CREDENTIAL_FIELDS) {
      const value = credentials[field.key]?.trim()
      if (value) out[field.header] = value
    }
    return out
  }

  function clear() {
    for (const key of Object.keys(credentials)) credentials[key] = ''
  }

  return { fields: CREDENTIAL_FIELDS, credentials, activeCount, headers, clear }
}
