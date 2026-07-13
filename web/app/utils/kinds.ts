// Canonical kind metadata. One source of truth for labels, card shape, the
// detail-route base, and which kinds support upstream discovery. Keep this in
// sync with the backend's canonical `kind` values.

export type CardShape = 'poster' | 'portrait' | 'square' | 'landscape'

export interface KindMeta {
  /** canonical kind emitted by the API */
  kind: string
  /** short human label */
  label: string
  /** plural label for listings/rails */
  plural: string
  /** artwork shape for cards */
  shape: CardShape
  /** detail-route builder segment, e.g. `movies` in `/movies/:id`; empty = fallback */
  route: string
  /** whether the kind can be discovered upstream from the search workbench */
  discoverable: boolean
}

export const KINDS: KindMeta[] = [
  { kind: 'movie', label: 'Movie', plural: 'Movies', shape: 'poster', route: 'movies', discoverable: true },
  { kind: 'tv_show', label: 'TV show', plural: 'TV shows', shape: 'poster', route: 'tv', discoverable: true },
  { kind: 'anime', label: 'Anime', plural: 'Anime', shape: 'poster', route: 'anime', discoverable: true },
  { kind: 'artist', label: 'Artist', plural: 'Artists', shape: 'portrait', route: 'artists', discoverable: true },
  { kind: 'release_group', label: 'Album', plural: 'Albums', shape: 'square', route: 'albums', discoverable: true },
  { kind: 'release', label: 'Release', plural: 'Releases', shape: 'square', route: 'releases', discoverable: false },
  { kind: 'recording', label: 'Recording', plural: 'Recordings', shape: 'square', route: 'recordings', discoverable: true },
  { kind: 'book_work', label: 'Book', plural: 'Books', shape: 'poster', route: 'books', discoverable: true },
  { kind: 'book_edition', label: 'Book edition', plural: 'Book editions', shape: 'poster', route: '', discoverable: false },
  { kind: 'manga', label: 'Manga', plural: 'Manga', shape: 'poster', route: 'manga', discoverable: true },
  { kind: 'manga_edition', label: 'Manga edition', plural: 'Manga editions', shape: 'poster', route: '', discoverable: false },
  { kind: 'manga_volume', label: 'Manga volume', plural: 'Manga volumes', shape: 'poster', route: 'manga/volumes', discoverable: true },
  { kind: 'comic', label: 'Comic', plural: 'Comics', shape: 'poster', route: '', discoverable: false },
  { kind: 'comic_volume', label: 'Comic volume', plural: 'Comic volumes', shape: 'poster', route: 'comics/volumes', discoverable: true },
]

const BY_KIND = new Map(KINDS.map(item => [item.kind, item]))

export function kindMeta(kind?: string | null): KindMeta | undefined {
  return kind ? BY_KIND.get(kind) : undefined
}

export function kindLabel(kind?: string | null): string {
  return kindMeta(kind)?.label ?? titleCaseKey(kind ?? '')
}

export function cardShape(kind?: string | null): CardShape {
  return kindMeta(kind)?.shape ?? 'poster'
}

/** Kinds offered as domain filters in search/browse dropdowns. */
export const FILTER_KINDS = KINDS.filter(k => k.route || k.discoverable)

/** Kinds that can be resolved from an upstream provider. */
export const DISCOVERABLE_KINDS = KINDS.filter(k => k.discoverable)

// Local copy to avoid importing display.ts (keeps this module dependency-free
// for the kindLabel fallback).
function titleCaseKey(value: string): string {
  return value
    .replace(/[_-]+/g, ' ')
    .trim()
    .replace(/\b\w/g, letter => letter.toUpperCase())
}
