import { describe, expect, it } from 'vitest'
import type { DiscoveryCandidate } from '../app/utils/types'
import {
  candidateActiveSpan,
  candidateAliases,
  candidateConfidencePercent,
  candidateEvidence,
  candidateEvidenceSummary,
  candidateFacts,
  candidateHasIdentityDetails,
  candidateInitial,
  candidateKind,
  candidateName,
} from '../app/utils/discoveryCandidate'

function candidate(overrides: Partial<DiscoveryCandidate> = {}): DiscoveryCandidate {
  return {
    candidate_ref: '00000000-0000-0000-0000-000000000001',
    rank: 1,
    confidence: 0.6,
    match: 'possible',
    display: { name: 'Unlucky Morpheus', type: 'group' },
    evidence: [],
    ...overrides,
  }
}

describe('discovery candidate presentation', () => {
  it('builds aligned identity facts from the distinguishing artist fields', () => {
    const value = candidate({
      display: {
        name: 'Unlucky Morpheus',
        type: 'group',
        area: 'Japan',
        begin_date: '2008',
        aliases: ['Unlucky Morpheus', 'Ankimo'],
        genres: ['j-rock', 'metal'],
        release_count: 28,
        fan_count: 4707,
      },
    })

    expect(candidateName(value)).toBe('Unlucky Morpheus')
    expect(candidateInitial(value)).toBe('U')
    expect(candidateKind(value, 'Artist')).toBe('Group')
    expect(candidateActiveSpan(value)).toBe('2008–present')
    expect(candidateAliases(value)).toEqual(['Ankimo'])
    expect(candidateFacts(value)).toEqual([
      { label: 'Type', value: 'Group' },
      { label: 'Location', value: 'Japan' },
      { label: 'Active', value: '2008–present' },
      { label: 'Catalog', value: '28 releases' },
      { label: 'Audience', value: '4,707 fans' },
    ])
    expect(candidateHasIdentityDetails(value)).toBe(true)
  })

  it('turns scoring internals into concise user-facing evidence', () => {
    const value = candidate({
      evidence: [
        { field: 'provider_score', outcome: 'support', weight: 0.2 },
        { field: 'name', outcome: 'exact', weight: 0.4 },
        { field: 'identity_crosswalk', outcome: 'explicit_relationship', weight: 0.39 },
        { field: 'country', outcome: 'mismatch', weight: -0.08 },
      ],
    })

    expect(candidateEvidence(value)).toEqual([
      { label: 'Upstream relevance', tone: 'positive' },
      { label: 'Exact name', tone: 'positive' },
      { label: 'Authoritative cross-link', tone: 'positive' },
      { label: 'Country: does not match', tone: 'negative' },
    ])
    expect(candidateEvidenceSummary(value)).toBe('Some supporting evidence')
    expect(candidateConfidencePercent(value)).toBe(60)
  })

  it('distinguishes an otherwise empty same-name record from one with context', () => {
    const sparse = candidate({ display: { name: 'Unlucky Morpheus', type: 'artist' } })
    expect(candidateHasIdentityDetails(sparse)).toBe(false)
    expect(candidateConfidencePercent(candidate({ confidence: 1.4 }))).toBe(100)
    expect(candidateConfidencePercent(candidate({ confidence: -1 }))).toBe(0)
  })
})
