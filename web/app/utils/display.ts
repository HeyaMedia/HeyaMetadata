// Centralized, defensive formatting helpers. The single hard rule: no
// user-facing path may ever emit "[object Object]". Every helper here degrades
// to an empty string for values it does not understand, so callers can simply
// `v-if` on the result. Structured records (ratings, credits, external IDs) get
// dedicated helpers/components; formatValue is the safe generic fallback.

const MAX_DEPTH = 6

// Keys that, when present with a primitive value, represent the object's
// human label. Ordered by preference.
const LABEL_KEYS = ['value', 'name', 'title', 'display_name', 'label', 'text']

/** Turn a snake/kebab/camel key into Title Case, e.g. `release_year` → `Release Year`. */
export function formatKey(key: string): string {
  return String(key)
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .replace(/[_-]+/g, ' ')
    .trim()
    .replace(/\b\w/g, letter => letter.toUpperCase())
}

/** Title-case a free value like a status or type token (`in_production` → `In Production`). */
export function titleCase(value: unknown): string {
  const text = formatValue(value)
  if (!text) return ''
  return text.replace(/[_-]+/g, ' ').replace(/\b\w/g, letter => letter.toUpperCase())
}

/**
 * Recursively coerce any value to a concise, human-readable string.
 * - primitives return directly (booleans become Yes/No);
 * - arrays are formatted element-wise and joined;
 * - objects resolve to a label key when possible, otherwise a compact
 *   `Key: value` join of their primitive fields;
 * - unknown/empty objects return '' (they are omitted, never coerced).
 */
export function formatValue(input: unknown): string {
  return format(input, 0)
}

function format(value: unknown, depth: number): string {
  if (value == null) return ''
  const type = typeof value
  if (type === 'string') return (value as string).trim()
  if (type === 'number') return Number.isFinite(value) ? String(value) : ''
  if (type === 'bigint') return String(value)
  if (type === 'boolean') return value ? 'Yes' : 'No'
  if (type !== 'object') return ''
  if (depth >= MAX_DEPTH) return ''

  if (Array.isArray(value)) {
    const parts: string[] = []
    for (const item of value) {
      const text = format(item, depth + 1)
      if (text) parts.push(text)
    }
    return parts.join(', ')
  }

  const obj = value as Record<string, unknown>
  for (const key of LABEL_KEYS) {
    if (key in obj) {
      const text = format(obj[key], depth + 1)
      if (text) return text
    }
  }
  // Generic composite: join the first few primitive fields so we never lose
  // data to coercion, but never surface raw nested objects.
  const parts: string[] = []
  for (const [key, item] of Object.entries(obj)) {
    const itemType = typeof item
    if (item == null) continue
    if (itemType === 'string' || itemType === 'number' || itemType === 'boolean' || itemType === 'bigint') {
      const text = format(item, depth + 1)
      if (text) parts.push(`${formatKey(key)}: ${text}`)
    }
    if (parts.length >= 4) break
  }
  return parts.join(' · ')
}

/** Format an array with an explicit per-item formatter, dropping empties. */
export function formatList<T>(items: T[] | null | undefined, formatter: (item: T) => string, separator = ', '): string {
  if (!Array.isArray(items)) return ''
  return items.map(formatter).filter(Boolean).join(separator)
}

/** First non-empty formatted value among the arguments. */
export function firstValue(...values: unknown[]): string {
  for (const value of values) {
    const text = formatValue(value)
    if (text) return text
  }
  return ''
}

// ---- Domain-shaped helpers -------------------------------------------------

export interface LocalizedText {
  value?: string
  language?: string
  country?: string
  type?: string
  primary?: boolean
}

function comparableLanguage(value: string | null | undefined): string {
  const normalized = String(value ?? '').trim().toLowerCase().replace('_', '-')
  const base = normalized.split('-')[0]
  const aliases: Record<string, string> = {
    eng: 'en', jpn: 'ja', ger: 'de', deu: 'de', fre: 'fr', fra: 'fr',
    spa: 'es', ita: 'it', por: 'pt', kor: 'ko', chi: 'zh', zho: 'zh',
  }
  return aliases[base] ?? base
}

/** Pick a display title, honoring the page's requested language when supplied. */
export function preferredText(items: LocalizedText[] | null | undefined, languages: string[] = []): string {
  if (!Array.isArray(items) || !items.length) return ''
  const requested = languages.map(comparableLanguage).filter(Boolean)
  if (requested[0]) {
    const localized = items.find(item => comparableLanguage(item?.language) === requested[0] && formatValue(item?.value))
    if (localized) return formatValue(localized.value)
  }
  const untagged = items.find(item => !comparableLanguage(item?.language) && formatValue(item?.value))
  if (untagged) return formatValue(untagged.value)
  for (const language of requested.slice(1)) {
    const localized = items.find(item => comparableLanguage(item?.language) === language && formatValue(item?.value))
    if (localized) return formatValue(localized.value)
  }
  const primary = items.find(item => item?.primary || item?.type === 'display' || item?.type === 'main')
  return formatValue((primary ?? items[0])?.value)
}

export interface ArtistCredit {
  name?: string
  artist_name?: string
  join_phrase?: string
}

/** Render a MusicBrainz-style artist-credit array as a single credit line. */
export function artistCreditLine(credits: ArtistCredit[] | null | undefined): string {
  if (!Array.isArray(credits)) return ''
  return credits
    .map(credit => formatValue(credit?.name ?? credit?.artist_name))
    .filter(Boolean)
    .join(', ')
}

export interface Rating {
  system?: string
  provider?: string
  value?: number
  scale_max?: number
  votes?: number
}

/** `8.7 / 10` style value for a rating, empty when there is nothing to show. */
export function ratingValue(rating: Rating | null | undefined): string {
  if (!rating || rating.value == null) return ''
  const scale = rating.scale_max ? ` / ${rating.scale_max}` : ''
  return `${rating.value}${scale}`
}

export function externalIdLabel(external: { provider?: string; namespace?: string; value?: string } | null | undefined): string {
  if (!external) return ''
  return [external.provider, external.namespace].filter(Boolean).map(formatKey).join(' · ')
}

/** Humanize a snake_case token without recasing, e.g. `electric_bass_guitar` → `electric bass guitar`. */
export function humanizeToken(value: unknown): string {
  return formatValue(value).replace(/[_]+/g, ' ').trim()
}

/** English display name for a language code (`eng`/`en` → `English`); falls back to the raw code. */
export function languageName(code: unknown): string {
  const text = formatValue(code)
  if (!text) return ''
  try {
    return new Intl.DisplayNames(['en'], { type: 'language' }).of(text) || text
  } catch {
    return text
  }
}

/** English display name for an ISO 15924 script code (`Latn` → `Latin`); falls back to the raw code. */
export function scriptName(code: unknown): string {
  const text = formatValue(code)
  if (!text) return ''
  try {
    return new Intl.DisplayNames(['en'], { type: 'script' }).of(text) || text
  } catch {
    return text
  }
}

// ---- Numeric / temporal ----------------------------------------------------

/** Human runtime from whole minutes, e.g. 136 → `2h 16m`. */
export function formatRuntime(minutes: unknown): string {
  const total = typeof minutes === 'number' ? minutes : Number(minutes)
  if (!Number.isFinite(total) || total <= 0) return ''
  const hours = Math.floor(total / 60)
  const mins = Math.round(total % 60)
  if (hours && mins) return `${hours}h ${mins}m`
  if (hours) return `${hours}h`
  return `${mins}m`
}

/** Human duration from milliseconds, e.g. 214000 → `3:34`. */
export function formatDuration(ms: unknown): string {
  const total = typeof ms === 'number' ? ms : Number(ms)
  if (!Number.isFinite(total) || total <= 0) return ''
  const seconds = Math.round(total / 1000)
  const mins = Math.floor(seconds / 60)
  const secs = seconds % 60
  return `${mins}:${String(secs).padStart(2, '0')}`
}

/** Compact integer formatting with thousands separators. */
export function formatCount(value: unknown): string {
  const num = typeof value === 'number' ? value : Number(value)
  if (!Number.isFinite(num)) return ''
  return num.toLocaleString('en-US')
}

/** Format an ISO-ish date string to a locale date; falls back to the raw year. */
export function formatDate(value: unknown): string {
  const text = formatValue(value)
  if (!text) return ''
  const parsed = new Date(text)
  if (Number.isNaN(parsed.getTime())) return text
  // Date-only strings should not shift across timezones.
  if (/^\d{4}-\d{2}-\d{2}$/.test(text)) {
    return parsed.toLocaleDateString('en-US', { year: 'numeric', month: 'long', day: 'numeric', timeZone: 'UTC' })
  }
  return parsed.toLocaleDateString('en-US', { year: 'numeric', month: 'long', day: 'numeric' })
}

export function percent(value: unknown): string {
  const num = typeof value === 'number' ? value : Number(value)
  if (!Number.isFinite(num)) return ''
  return `${Math.round(num * 100)}%`
}
