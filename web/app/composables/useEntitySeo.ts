import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { kindLabel } from '../utils/kinds'
import {
  entityBestImageId,
  entityDisplayTitle,
  entityRawDescription,
  entitySchemaNodes,
  imageVariantUrl,
  metaDescription,
} from '../utils/schemaOrg'
import type { EntityDocument } from '../utils/types'

// Reactive, client-side SEO for every entity that renders through EntityDetail.
// Detail data loads async after mount, so every field is a getter: while the
// entity is null we return `undefined` and let the global title/description
// defaults stand rather than emitting a "Loading…" title.

type OgType = 'website' | 'article' | 'book' | 'profile'
  | 'music.song' | 'music.album' | 'video.movie' | 'video.tv_show'

/** og:type for a canonical kind; unmapped kinds fall back to a generic website. */
function ogTypeForKind(kind?: string): OgType {
  switch (kind) {
    case 'movie': return 'video.movie'
    case 'tv_show':
    case 'anime': return 'video.tv_show'
    case 'release_group':
    case 'release': return 'music.album'
    case 'recording': return 'music.song'
    case 'book_work':
    case 'manga':
    case 'manga_volume':
    case 'comic_volume': return 'book'
    case 'person': return 'profile'
    default: return 'website'
  }
}

export function useEntitySeo(entity: MaybeRefOrGetter<EntityDocument | null>) {
  const site = useSiteConfig()
  const route = useRoute()

  const canonicalUrl = () => `${site.url}${route.path}`
  const ogImage = (): string | undefined => {
    const id = entityBestImageId(toValue(entity))
    return id ? imageVariantUrl(site.url, id) : undefined
  }

  const title = (): string | undefined => {
    const doc = toValue(entity)
    const base = entityDisplayTitle(doc)
    if (!doc || !base) return undefined
    const year = doc.display?.year
    return typeof year === 'number' && Number.isFinite(year) ? `${base} (${year})` : base
  }

  const description = (): string | undefined => {
    const doc = toValue(entity)
    if (!doc) return undefined
    return metaDescription(
      entityRawDescription(doc),
      `Canonical ${kindLabel(doc.kind)} metadata combined from every source, on Heya.`,
    )
  }

  useSeoMeta({
    title,
    description,
    ogImage,
    ogType: () => ogTypeForKind(toValue(entity)?.kind),
    twitterCard: () => (ogImage() ? 'summary_large_image' : 'summary'),
  })

  // A ref keeps the JSON-LD reactive: nuxt-schema-org re-resolves the graph when
  // the entity finally loads (see useSchemaOrg's isRef branch).
  useSchemaOrg(computed(() => entitySchemaNodes(toValue(entity), canonicalUrl(), ogImage())))
}
