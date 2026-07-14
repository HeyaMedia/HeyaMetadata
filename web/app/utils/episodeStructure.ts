export interface EpisodeNumbering {
  scheme?: string
  season?: number
  number?: number
  provider?: string
}

export interface EpisodeStructure {
  id?: string
  season_id?: string
  is_special?: boolean
  numbers?: EpisodeNumbering[]
}

export interface SeasonStructure {
  id?: string
  number?: number
  episode_ids?: string[]
}

function finiteNumber(value: unknown): number | null {
  const number = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(number) ? number : null
}

/**
 * Resolve an episode's canonical Heya season. Provider numbering is only a
 * fallback: season_id and the season's episode_ids are the canonical links.
 */
export function canonicalEpisodeSeason(episode: EpisodeStructure, seasons: SeasonStructure[] | null | undefined): number | null {
  const declared = Array.isArray(seasons) ? seasons : []
  if (episode?.season_id) {
    const season = declared.find(item => item?.id === episode.season_id)
    const number = finiteNumber(season?.number)
    if (number != null) return number
  }
  if (episode?.id) {
    const season = declared.find(item => Array.isArray(item?.episode_ids) && item.episode_ids.includes(episode.id!))
    const number = finiteNumber(season?.number)
    if (number != null) return number
  }
  if (episode?.is_special) return 0
  return canonicalEpisodeNumber(episode)?.season ?? null
}

/** Pick the displayed episode number within an already-resolved Heya season. */
export function canonicalEpisodeNumber(episode: EpisodeStructure, season?: number | null): EpisodeNumbering | null {
  const numbers = Array.isArray(episode?.numbers)
    ? episode.numbers.filter(item => item && finiteNumber(item.number) != null)
    : []
  if (!numbers.length) return null

  if (season != null) {
    const inSeason = (item: EpisodeNumbering) => finiteNumber(item.season) === season
    return numbers.find(item => item.scheme === 'aired' && inSeason(item))
      ?? numbers.find(item => item.scheme !== 'absolute' && inSeason(item))
      ?? numbers.find(inSeason)
      ?? null
  }
  return numbers.find(item => item.scheme === 'aired')
    ?? numbers.find(item => item.scheme !== 'absolute' && item.season != null)
    ?? numbers[0]
    ?? null
}

/**
 * Provider numbering is useful evidence, but every provider also contributes
 * an `aired` alias and reconciled sources can repeat an identical scheme.
 * Keep those details in the canonical document while presenting one compact
 * row per meaningful numbering scheme in the UI.
 */
export function displayEpisodeNumbers(numbers: EpisodeNumbering[] | null | undefined): EpisodeNumbering[] {
  if (!Array.isArray(numbers)) return []
  const seen = new Set<string>()
  const result: EpisodeNumbering[] = []
  for (const item of numbers) {
    const scheme = String(item?.scheme ?? '').trim().toLowerCase()
    const number = finiteNumber(item?.number)
    if (!scheme || scheme === 'aired' || number == null) continue
    const season = finiteNumber(item?.season)
    const key = `${scheme}:${season ?? ''}:${number}`
    if (seen.has(key)) continue
    seen.add(key)
    result.push({ ...item, scheme, number, ...(season == null ? {} : { season }) })
  }
  return result
}
