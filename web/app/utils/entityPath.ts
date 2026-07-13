import { kindMeta } from './kinds'

export interface EntityRef {
  id?: string
  kind?: string | null
  entity_id?: string
}

/**
 * Map a canonical entity to its detail URL. This is the single mapping from
 * `kind` to route used by search, latest, browse, collections, and relations.
 * IDs are opaque Heya UUIDs; provider IDs must never be used here.
 *
 * Unknown or route-less kinds fall back to `/entities/:id` so future kinds
 * remain linkable without a code change.
 */
export function entityPath(entity: EntityRef | null | undefined): string {
  if (!entity) return '/'
  const id = entity.id ?? entity.entity_id
  if (!id) return '/'
  const meta = kindMeta(entity.kind)
  if (meta && meta.route) return `/${meta.route}/${id}`
  return `/entities/${id}`
}
