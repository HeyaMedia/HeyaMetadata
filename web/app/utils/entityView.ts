import { formatValue } from './display'
import { kindLabel } from './kinds'
import type { EntityDocument, EntitySummary } from './types'

// Shared extraction of card-facing fields from either a compact summary or a
// full document, so every card/rail/result renders identical copy.

type AnyEntity = EntitySummary | EntityDocument | Record<string, any>

export function entityTitle(entity: AnyEntity | null | undefined): string {
  if (!entity) return 'Untitled'
  const display = (entity as any).display ?? {}
  const presentation = (entity as any).presentation ?? {}
  return formatValue(presentation.title || display.title || display.name) || 'Untitled'
}

export function entityImageId(entity: AnyEntity | null | undefined): string | undefined {
  if (!entity) return undefined
  const display = (entity as any).display ?? {}
  const images = (entity as any).presentation?.images ?? {}
  return display.image_id || images.poster || images.cover || images.profile || images.primary
}

/** Concise supporting line: year, artist credit, and disambiguation when present. */
export function entitySubtitle(entity: AnyEntity | null | undefined): string {
  if (!entity) return ''
  const display = (entity as any).display ?? {}
  const parts: string[] = []
  if (display.artist_credit) parts.push(formatValue(display.artist_credit))
  if (display.year) parts.push(String(display.year))
  if (display.disambiguation) parts.push(formatValue(display.disambiguation))
  return parts.filter(Boolean).join(' · ')
}

export function entityKindLabel(entity: AnyEntity | null | undefined): string {
  return kindLabel((entity as any)?.kind)
}
