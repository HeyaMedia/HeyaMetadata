// Shared shapes for the Heya Metadata API responses. These are intentionally
// permissive (`data` is domain-specific and only ever read defensively through
// the display helpers), but the envelope fields are stable across kinds.

export interface Freshness {
  state?: string
  updated_at?: string
  fresh_until?: string
  providers?: Record<string, unknown>
}

export interface EntityDisplay {
  title?: string
  name?: string
  original_title?: string
  year?: number
  image_id?: string
  disambiguation?: string
  artist_credit?: string
  [key: string]: unknown
}

/** Compact summary returned by search / browse / latest. */
export interface EntitySummary {
  id: string
  kind: string
  slug?: string
  display?: EntityDisplay
  freshness?: Freshness
  schema_version?: number
  projection_version?: number
}

export interface Presentation {
  title?: string
  title_language?: string
  description?: string
  description_language?: string
  tagline?: string
  images?: Record<string, string>
  language_preferences?: string[]
}

export interface ExternalId {
  provider?: string
  namespace?: string
  value?: string
  evidence?: string
}

/** Full localized entity document. */
export interface EntityDocument {
  id: string
  kind: string
  slug?: string
  display?: EntityDisplay
  presentation?: Presentation
  data?: Record<string, any>
  external_ids?: ExternalId[]
  provenance?: Record<string, ProvenanceSource[]>
  freshness?: Freshness
  projection_version?: number
  schema_version?: number
}

export interface ProvenanceSource {
  provider?: string
  observation_id?: string
  [key: string]: unknown
}

export interface ImageCandidate {
  id: string
  class?: string
  language?: string
  width?: number
  height?: number
  provider?: string
  provider_score?: number
  materialization_state?: string
  selected?: boolean
  selection_reason?: string
}

export interface ImagesResponse {
  language_preferences?: string[]
  selections?: Record<string, string>
  results?: ImageCandidate[]
}

export interface CollectionMember {
  provider_id: string
  entity_id?: string
  title?: string
  year?: number
  image_id?: string
  order?: number
}

export interface CollectionCard {
  provider?: string
  provider_id: string
  name?: string
  overview?: string
  image_id?: string
  members?: CollectionMember[]
}

export interface LibraryStats {
  entities?: number
  kinds?: Record<string, number>
  provider_claims?: Record<string, number>
  images?: number
  materialized_images?: number
  provider_records?: number
  fresh?: number
  stale?: number
  generated_at?: string
}

export interface BrowseResult {
  results: EntitySummary[]
  total: number
  offset: number
  limit: number
}

export interface Credit {
  provider?: string
  character?: string
  job?: string
  credit_type?: string
  display_name?: string
  profile_image_id?: string
  provider_person_id?: string
  person_entity_id?: string
  order?: number
}

export interface Relation {
  id?: string
  relation_type?: string
  source_kind?: string
  target_kind?: string
  target_entity_id?: string
  provider?: string
  namespace?: string
  provider_value?: string
  metadata?: Record<string, any>
  last_observed_at?: string
}

export interface RelationsResponse {
  relations: Relation[]
  total?: number
  offset?: number
  limit?: number
}

export interface LyricDocument {
  id?: string
  provider?: string
  track_name?: string
  artist_name?: string
  album_name?: string
  duration_ms?: number
  instrumental?: boolean
  plain_lyrics?: string
  synced_lyrics?: string
}

export interface AuthUser {
  id: string
  username: string
  role?: string
  created_at?: string
}

export interface PersonRef {
  display_name?: string
  profile_image_id?: string
  provider?: string
  provider_person_id?: string
  entity_id?: string
}

/** Canonical person document from GET /api/v2/persons/{id}. */
export interface PersonDocument {
  id: string
  kind?: string
  slug?: string
  display?: { title?: string; image_id?: string }
  external_ids?: ExternalId[]
  data?: {
    names?: string[]
    credits?: PersonCredit[]
    credit_total?: number
    [key: string]: any
  }
}

export interface PersonCredit {
  entity_id?: string
  provider?: string
  provider_target_id?: string
  kind: string
  title?: string
  year?: number
  image_id?: string
  credit_type?: string
  character?: string
  department?: string
  job?: string
  order?: number
}

export interface PersonCreditsResponse {
  person: PersonRef
  credits: PersonCredit[]
  total?: number
}

export interface ShowRef {
  entity_id: string
  kind?: string
  title?: string
  image_id?: string
}

export interface SeasonResource {
  id: string
  show?: ShowRef
  data?: Record<string, any>
  episodes?: Array<Record<string, any>> | null
}

export interface EpisodeResource {
  id: string
  show?: ShowRef
  data?: Record<string, any>
}

export interface DiscoveryCandidate {
  rank: number
  confidence: number
  match: string
  identity: { provider: string; namespace: string; value: string }
  display: Record<string, any>
  evidence?: Array<Record<string, any>>
  existing_entity_id?: string
  resolution: { kind: string; provider: string; namespace: string; value: string }
}
