// Client-side SEO helpers shared by the entity SEO composable and the four
// custom detail pages. Everything here is pure and defensive: it degrades to an
// empty string / empty array for values it does not understand, so callers can
// feed it partially-loaded documents without guarding every field themselves.

import { firstValue, formatValue } from './display'
import type { EntityDocument } from './types'

// Image classes preferred for a large social/preview image, most landscape-
// friendly first. Falls back to display.image_id when none of these are present.
const IMAGE_CLASS_ORDER = ['backdrop', 'cover', 'poster', 'portrait', 'banner'] as const

/** Best display title for an entity document, without any year suffix. */
export function entityDisplayTitle(entity: EntityDocument | null | undefined): string {
  const display = entity?.display
  return firstValue(entity?.presentation?.title, display?.title, display?.name, display?.original_title)
}

/** Raw (untruncated) description source: localized description, else tagline. */
export function entityRawDescription(entity: EntityDocument | null | undefined): string {
  return firstValue(entity?.presentation?.description, entity?.presentation?.tagline)
}

/** Best image id for a social/preview image, preferring landscape/large classes. */
export function entityBestImageId(entity: EntityDocument | null | undefined): string {
  const images = entity?.presentation?.images
  if (images) {
    for (const imageClass of IMAGE_CLASS_ORDER) {
      const id = formatValue(images[imageClass])
      if (id) return id
    }
  }
  return formatValue(entity?.display?.image_id)
}

/** Absolute URL for an artwork variant, e.g. for og:image / schema image. */
export function imageVariantUrl(baseUrl: string, imageId: string, width = 1200): string {
  return `${baseUrl}/api/v2/images/${imageId}/variants/webp/${width}`
}

/**
 * Single-line meta description: the raw text collapsed to one line and truncated
 * at a word boundary with an ellipsis, or `fallback` when the text is empty.
 */
export function metaDescription(raw: unknown, fallback: string, max = 160): string {
  const text = formatValue(raw).replace(/\s+/g, ' ').trim()
  if (!text) return fallback
  if (text.length <= max) return text
  const clipped = text.slice(0, max)
  const lastSpace = clipped.lastIndexOf(' ')
  const head = (lastSpace > 0 ? clipped.slice(0, lastSpace) : clipped).replace(/[\s.,;:!?-]+$/, '')
  return `${head}…`
}

/**
 * Map a canonical entity to schema.org node(s) for JSON-LD injection. Uses the
 * nuxt-schema-org `define*` helpers where the task names them (Movie/Book/Person/
 * WebPage) and falls back to raw typed nodes for the rest. Always returns an
 * array (empty when there is nothing to describe) and never throws on missing
 * data. `absoluteUrl` is the canonical page URL; `imageUrl` an absolute artwork
 * URL (both already resolved by the caller).
 */
export function entitySchemaNodes(
  entity: EntityDocument | null | undefined,
  absoluteUrl: string,
  imageUrl?: string,
): Array<Record<string, any>> {
  const name = entityDisplayTitle(entity)
  if (!entity || !name) return []

  // Fields common to every node shape; empties are omitted below.
  const base: Record<string, any> = { name, url: absoluteUrl }
  const description = entityRawDescription(entity)
  if (description) base.description = description
  if (imageUrl) base.image = imageUrl

  switch (entity.kind) {
    case 'movie':
      return [defineMovie(base)]
    case 'book_work':
    case 'book_edition':
    case 'manga':
    case 'manga_edition':
    case 'manga_volume':
      return [defineBook(base)]
    case 'person':
      return [definePerson(base)]
    case 'tv_show':
    case 'anime':
      return [{ '@type': 'TVSeries', ...base }]
    case 'artist':
      return [{ '@type': 'MusicGroup', ...base }]
    case 'release_group':
    case 'release':
      return [{ '@type': 'MusicAlbum', ...base }]
    case 'recording':
      return [{ '@type': 'MusicRecording', ...base }]
    case 'comic':
    case 'comic_volume':
      return [{ '@type': 'ComicSeries', ...base }]
    default:
      return [defineWebPage(base)]
  }
}
