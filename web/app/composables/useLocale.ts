import { reactive } from 'vue'

// Request locale used for entity presentation and artwork selection. Memory-only,
// shared across the app so the header controls and every detail page agree.

const locale = reactive({
  language: 'en',
  fallbackLanguages: 'ja, en',
  country: '',
})

export function useLocale() {
  /** Locale query params for entity/image endpoints. */
  function query(): URLSearchParams {
    const params = new URLSearchParams()
    if (locale.language.trim()) params.set('language', locale.language.trim())
    if (locale.fallbackLanguages.trim()) params.set('fallback_languages', locale.fallbackLanguages.trim())
    if (locale.country.trim()) params.set('country', locale.country.trim().toUpperCase())
    return params
  }

  /** Accept-Language header for read requests. */
  function headers(): Record<string, string> {
    const languages = [locale.language, ...locale.fallbackLanguages.split(',')]
      .map(value => value.trim())
      .filter((value, index, values) => value && values.indexOf(value) === index)
    return languages.length ? { 'Accept-Language': languages.join(', ') } : {}
  }

  /** Stable string signature for cache keys / watchers. */
  function signature(): string {
    return `${locale.language}|${locale.fallbackLanguages}|${locale.country}`
  }

  return { locale, query, headers, signature }
}
