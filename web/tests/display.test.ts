import { describe, expect, it } from 'vitest'
import {
  artistCreditLine,
  externalIdLabel,
  firstValue,
  formatDuration,
  formatKey,
  formatRuntime,
  formatValue,
  preferredText,
  ratingValue,
  titleCase,
} from '../app/utils/display'

// The one hard guarantee: nothing here may ever produce "[object Object]".
function assertNoObjectObject(text: string) {
  expect(text).not.toContain('[object Object]')
}

describe('formatValue — primitives', () => {
  it('returns strings, numbers, and booleans directly', () => {
    expect(formatValue('Naruto')).toBe('Naruto')
    expect(formatValue('  padded  ')).toBe('padded')
    expect(formatValue(1999)).toBe('1999')
    expect(formatValue(0)).toBe('0')
    expect(formatValue(true)).toBe('Yes')
    expect(formatValue(false)).toBe('No')
  })

  it('treats null, undefined, NaN, and Infinity as empty', () => {
    expect(formatValue(null)).toBe('')
    expect(formatValue(undefined)).toBe('')
    expect(formatValue(Number.NaN)).toBe('')
    expect(formatValue(Number.POSITIVE_INFINITY)).toBe('')
  })
})

describe('formatValue — localized titles', () => {
  const titles = [
    { language: 'ja', type: 'original', value: 'ナルト' },
    { language: 'en', type: 'display', value: 'Naruto', primary: true },
  ]

  it('formats an array of localized text objects', () => {
    const result = formatValue(titles)
    expect(result).toBe('ナルト, Naruto')
    assertNoObjectObject(result)
  })

  it('preferredText picks the primary/display entry', () => {
    expect(preferredText(titles)).toBe('Naruto')
    expect(preferredText([{ value: 'Solo' }])).toBe('Solo')
    expect(preferredText([])).toBe('')
  })
})

describe('formatValue — artist credits', () => {
  const credits = [
    { artist_id: 'x', artist_name: 'Ado', name: 'Ado', position: 0 },
    { artist_id: 'y', artist_name: 'Guest', name: 'Guest', position: 1 },
  ]

  it('resolves credit objects via their label key', () => {
    const result = formatValue(credits)
    expect(result).toBe('Ado, Guest')
    assertNoObjectObject(result)
  })

  it('artistCreditLine joins names', () => {
    expect(artistCreditLine(credits)).toBe('Ado, Guest')
    expect(artistCreditLine(null)).toBe('')
  })
})

describe('formatValue — ratings', () => {
  const rating = { system: 'imdb', value: 8.7, scale_max: 10, votes: 2243093 }

  it('never coerces a rating object to [object Object]', () => {
    assertNoObjectObject(formatValue(rating))
    assertNoObjectObject(formatValue([rating]))
  })

  it('ratingValue renders value over scale', () => {
    expect(ratingValue(rating)).toBe('8.7 / 10')
    expect(ratingValue({ value: 82.38 })).toBe('82.38')
    expect(ratingValue(null)).toBe('')
    expect(ratingValue({})).toBe('')
  })
})

describe('formatValue — authors, genres, external IDs', () => {
  it('formats author objects', () => {
    const authors = [{ id: 'a', name: 'J.R.R. Tolkien', external_ids: [{ provider: 'openlibrary', value: 'OL26320A' }] }]
    expect(formatValue(authors)).toBe('J.R.R. Tolkien')
  })

  it('formats plain-string genre arrays', () => {
    expect(formatValue(['Action', 'Sci-Fi', 'Adventure'])).toBe('Action, Sci-Fi, Adventure')
  })

  it('formats weighted genre objects via name', () => {
    const genres = [{ name: 'j-pop', provider: 'musicbrainz', weight: 2 }]
    expect(formatValue(genres)).toBe('j-pop')
  })

  it('formats external IDs via the value key and labels them', () => {
    const external = { provider: 'imdb', namespace: 'title', value: 'tt0133093' }
    expect(formatValue(external)).toBe('tt0133093')
    expect(externalIdLabel(external)).toBe('Imdb · Title')
  })
})

describe('formatValue — arbitrary nested objects', () => {
  it('omits empty objects rather than coercing them', () => {
    expect(formatValue({})).toBe('')
    expect(formatValue({ nested: {}, list: [] })).toBe('')
    expect(formatValue([{}, {}])).toBe('')
  })

  it('joins unknown primitive fields without leaking object syntax', () => {
    const measurement = { budget: 63000000, currency: 'USD', extra: { deep: 1 } }
    const result = formatValue(measurement)
    assertNoObjectObject(result)
    expect(result).toContain('Budget: 63000000')
    expect(result).toContain('Currency: USD')
  })

  it('resolves a label key that is itself an object', () => {
    expect(formatValue({ name: { value: 'Deep Name' } })).toBe('Deep Name')
  })

  it('guards against deeply recursive structures', () => {
    const cyclic: Record<string, unknown> = { value: undefined }
    cyclic.self = cyclic
    const result = formatValue(cyclic)
    assertNoObjectObject(result)
  })
})

describe('helpers', () => {
  it('formatKey title-cases snake/camel keys', () => {
    expect(formatKey('release_year')).toBe('Release Year')
    expect(formatKey('scale_max')).toBe('Scale Max')
    expect(formatKey('originalTitle')).toBe('Original Title')
  })

  it('titleCase normalizes status tokens', () => {
    expect(titleCase('in_production')).toBe('In Production')
    expect(titleCase('released')).toBe('Released')
  })

  it('firstValue returns the first non-empty', () => {
    expect(firstValue(null, '', 0, 'fallback')).toBe('0')
    expect(firstValue(undefined, null, 'x')).toBe('x')
    expect(firstValue(null, undefined)).toBe('')
  })

  it('formatRuntime renders hours and minutes', () => {
    expect(formatRuntime(136)).toBe('2h 16m')
    expect(formatRuntime(60)).toBe('1h')
    expect(formatRuntime(45)).toBe('45m')
    expect(formatRuntime(0)).toBe('')
  })

  it('formatDuration renders m:ss from milliseconds', () => {
    expect(formatDuration(214000)).toBe('3:34')
    expect(formatDuration(0)).toBe('')
    expect(formatDuration('abc')).toBe('')
  })
})
