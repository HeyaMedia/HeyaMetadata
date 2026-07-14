import { describe, expect, it } from 'vitest'
import { canonicalEpisodeNumber, canonicalEpisodeSeason } from '../app/utils/episodeStructure'

const seasons = [
  { id: 'season-one', number: 1, episode_ids: ['episode-one'] },
  { id: 'season-two', number: 2, episode_ids: ['episode-twelve'] },
]

const secondCourEpisode = {
  id: 'episode-twelve',
  season_id: 'season-two',
  numbers: [
    { scheme: 'aired', season: 1, number: 12, provider: 'tmdb' },
    { scheme: 'tvdb', season: 1, number: 12, provider: 'tvdb' },
    { scheme: 'aired', season: 2, number: 1, provider: 'thexem' },
  ],
}

describe('canonical episodic structure', () => {
  it('uses the Heya season UUID instead of a flattened provider season', () => {
    const season = canonicalEpisodeSeason(secondCourEpisode, seasons)
    expect(season).toBe(2)
    expect(canonicalEpisodeNumber(secondCourEpisode, season)).toMatchObject({ season: 2, number: 1 })
  })

  it('falls back to canonical season episode membership', () => {
    const withoutSeasonId = { ...secondCourEpisode, season_id: undefined }
    expect(canonicalEpisodeSeason(withoutSeasonId, seasons)).toBe(2)
  })

  it('keeps explicitly typed specials in season zero', () => {
    expect(canonicalEpisodeSeason({ is_special: true, numbers: [{ scheme: 'special', number: 1 }] }, seasons)).toBe(0)
  })
})
