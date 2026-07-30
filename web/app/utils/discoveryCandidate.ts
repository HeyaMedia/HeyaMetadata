import type { DiscoveryCandidate } from './types'
import { formatCount, formatKey, formatValue, titleCase } from './display'

export interface CandidateFact {
  label: string
  value: string
}

export interface CandidateEvidence {
  label: string
  tone: 'positive' | 'neutral' | 'negative'
}

export function candidateName(candidate: DiscoveryCandidate): string {
  return formatValue(candidate.display?.title || candidate.display?.name) || 'Untitled'
}

export function candidateInitial(candidate: DiscoveryCandidate): string {
  return candidateName(candidate).trim().charAt(0).toLocaleUpperCase() || '?'
}

export function candidateKind(candidate: DiscoveryCandidate, fallback?: string): string {
  return titleCase(candidate.display?.type) || fallback || 'Identity'
}

export function candidateActiveSpan(candidate: DiscoveryCandidate): string {
  const display = candidate.display ?? {}
  if (display.begin_date && display.end_date) return `${display.begin_date}–${display.end_date}`
  if (display.begin_date && display.ended) return `Began ${display.begin_date}`
  if (display.begin_date) return `${display.begin_date}–present`
  if (display.end_date) return `Until ${display.end_date}`
  return ''
}

export function candidateFacts(candidate: DiscoveryCandidate): CandidateFact[] {
  const display = candidate.display ?? {}
  const facts: CandidateFact[] = []
  const add = (label: string, value: unknown) => {
    const formatted = formatValue(value)
    if (formatted) facts.push({ label, value: formatted })
  }

  add('Type', titleCase(display.type))
  add('Year', display.year)
  add('Location', display.area || display.country)
  add('Active', candidateActiveSpan(candidate))
  add('Artists', display.artists)
  if (display.release_count) add('Catalog', `${formatCount(display.release_count)} releases`)
  if (display.fan_count) add('Audience', `${formatCount(display.fan_count)} fans`)

  return facts
}

export function candidateAliases(candidate: DiscoveryCandidate): string[] {
  const name = candidateName(candidate).toLocaleLowerCase()
  return (candidate.display?.aliases ?? [])
    .filter(alias => alias.toLocaleLowerCase() !== name)
    .slice(0, 3)
}

export function candidateHasIdentityDetails(candidate: DiscoveryCandidate): boolean {
  const display = candidate.display ?? {}
  return Boolean(
    display.image_url
    || display.disambiguation
    || display.genres?.length
    || candidateAliases(candidate).length
    || candidateFacts(candidate).some(fact => fact.label !== 'Type'),
  )
}

function evidenceOutcome(value: unknown): string {
  const outcome = formatValue(value)
  if (!outcome) return ''
  const count = outcome.match(/^(\d+)_of_(\d+)$/)
  if (count) return `${count[1]} of ${count[2]} matched`
  const labels: Record<string, string> = {
    exact: 'exact match',
    support: 'supported',
    supported: 'supported',
    conflict: 'conflicts',
    mismatch: 'does not match',
  }
  return labels[outcome.toLocaleLowerCase()] ?? outcome.replaceAll('_', ' ')
}

export function candidateEvidence(candidate: DiscoveryCandidate): CandidateEvidence[] {
  return (candidate.evidence ?? []).slice(0, 4).map((fact) => {
    const field = formatValue(fact.field).toLocaleLowerCase()
    const outcome = evidenceOutcome(fact.outcome)
    const weight = Number(fact.weight)
    let label = [formatKey(field), outcome].filter(Boolean).join(': ')

    if ((field === 'name' || field === 'title') && outcome === 'exact match') label = 'Exact name'
    if (field === 'provider_score') label = 'Upstream relevance'
    if (field === 'identity_crosswalk') label = 'Authoritative cross-link'
    if (field === 'country' && outcome === 'exact match') label = 'Country matches'
    if (field === 'year' && outcome === 'exact match') label = 'Year matches'

    return {
      label: label || 'Supporting evidence',
      tone: Number.isFinite(weight) && weight < 0 ? 'negative' : Number.isFinite(weight) && weight > 0 ? 'positive' : 'neutral',
    }
  })
}

export function candidateEvidenceSummary(candidate: DiscoveryCandidate): string {
  const labels: Record<string, string> = {
    strong: 'Strong supporting evidence',
    likely: 'Likely match',
    possible: 'Some supporting evidence',
    weak: 'Limited evidence',
  }
  return labels[candidate.match] ?? 'Unranked evidence'
}

export function candidateConfidencePercent(candidate: DiscoveryCandidate): number {
  const confidence = Number(candidate.confidence)
  if (!Number.isFinite(confidence)) return 0
  return Math.round(Math.max(0, Math.min(1, confidence)) * 100)
}
