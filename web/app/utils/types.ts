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

/** materialized → the Heya target id is navigable; unresolved → display-only. */
export type ResolutionState = 'materialized' | 'unresolved'

export interface CollectionMember {
  entity_id?: string
  resolution_state?: ResolutionState
  title?: string
  year?: number
  image_id?: string
  order?: number
  /** passive provenance only — never used for routing */
  provider_id?: string
}

export interface CollectionCard {
  /** canonical Heya collection id — the only routing/read key */
  id: string
  name?: string
  overview?: string
  image_id?: string
  members?: CollectionMember[]
  /** passive provenance only */
  provider?: string
  provider_id?: string
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
  /** canonical person id — required; the only navigation key */
  person_entity_id: string
  character?: string
  job?: string
  credit_type?: string
  display_name?: string
  profile_image_id?: string
  order?: number
  /** passive provenance only */
  provider?: string
  provider_person_id?: string
}

export interface Relation {
  id?: string
  relation_type?: string
  source_kind?: string
  target_kind?: string
  target_entity_id?: string
  resolution_state?: ResolutionState
  metadata?: Record<string, any>
  last_observed_at?: string
  /** passive provenance only — never used for routing */
  provider?: string
  namespace?: string
  provider_value?: string
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

export interface TopTrack {
  rank: number
  title: string
  /** canonical recording UUID; present when materialized */
  recording_entity_id?: string
  resolution_state?: ResolutionState
  external_ids?: ExternalId[]
  playcount?: number
  listeners?: number
  url?: string
  /** passive provenance only */
  provider?: string
  provider_track_id?: string
}

export interface TopTrackSource {
  provider: string
  item_count?: number
  reported_total?: number
  truncated?: boolean
  source_observation_id?: string
  observed_at?: string
  projection_version?: number
}

export interface TopTracksResponse {
  artist_id: string
  results: TopTrack[]
  sources?: TopTrackSource[]
  total: number
  offset: number
  limit: number
}

export interface JobStateCount {
  state: string
  count: number
}

export interface AdminJob {
  id: number
  kind: string
  state: string
  queue: string
  attempt: number
  max_attempts: number
  priority: number
  created_at: string
  scheduled_at: string
  attempted_at?: string
  finalized_at?: string
  error?: string
  /** the job's own River payload */
  args?: Record<string, unknown>
}

export interface AdminJobsResponse {
  summary: JobStateCount[]
  jobs: AdminJob[]
  total: number
}

export type AdminJobAction = 'clear_completed' | 'clear_queue' | 'rescue_stuck'

export interface AdminJobActionResult {
  action: AdminJobAction
  affected: number
}

export interface AuthUser {
  id: string
  username: string
  role?: string
  created_at?: string
}

export interface ApiKey {
  id: string
  name: string
  prefix?: string
  /** Full secret — present only in the create response, shown once. */
  key?: string
  scopes?: string[]
  created_at?: string
  last_used_at?: string
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
  /** canonical id of the credited work; present when materialized */
  entity_id?: string
  resolution_state?: ResolutionState
  kind: string
  title?: string
  year?: number
  image_id?: string
  credit_type?: string
  character?: string
  department?: string
  job?: string
  order?: number
  /** passive provenance only */
  provider?: string
  provider_target_id?: string
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

/** Provider-transparent discovery candidate. The only actionable field is the
 * opaque candidate_ref, passed back to POST /resolutions. No provider identity,
 * existing-entity shortcut, or provider-shaped resolution object is exposed. */
export interface DiscoveryCandidate {
  candidate_ref: string
  rank: number
  confidence: number
  match: string
  display: Record<string, any>
  evidence?: Array<Record<string, any>>
}

/** The completed discovery result envelope (DiscoveryResource.result). */
export interface DiscoveryResult {
  status?: 'completed' | 'needs_selection'
  entity_id?: string
  candidates?: DiscoveryCandidate[]
  kind?: string
  warnings?: string[]
}
