import { useLocale } from './useLocale'
import { useProviderCredentials } from './useProviderCredentials'
import type {
  AdminJobAction,
  AdminJobActionResult,
  AdminJobsResponse,
  BrowseResult,
  CollectionCard,
  Credit,
  EntityDocument,
  EntitySummary,
  EpisodeResource,
  ImagesResponse,
  LibraryStats,
  LyricDocument,
  PersonCreditsResponse,
  PersonDocument,
  RelationsResponse,
  SeasonResource,
  TopTracksResponse,
} from '../utils/types'

// Same-origin `/api/v2` client. All reads flow through here so provider
// credentials and locale headers are applied consistently in one place.

const BASE = '/api/v2'

interface RequestOptions extends RequestInit {
  json?: boolean
}

// Reads RFC 9457 application/problem+json bodies (stable type/status/title/detail
// plus optional field errors) and falls back to the status line for anything else.
function messageFrom(body: any, response: Response): string {
  if (body && typeof body === 'object') {
    const base = body.detail || body.title || body.error || `${response.status} ${response.statusText}`
    const fields = Array.isArray(body.errors)
      ? body.errors.map((entry: any) => entry?.message || entry?.detail).filter(Boolean)
      : []
    return fields.length ? `${base} (${fields.join('; ')})` : base
  }
  return `${response.status} ${response.statusText}`
}

export function useHeyaApi() {
  const credentials = useProviderCredentials()
  const localeState = useLocale()

  function buildHeaders(json = false): Record<string, string> {
    const headers: Record<string, string> = {
      ...localeState.headers(),
      ...credentials.headers(),
    }
    if (json) headers['Content-Type'] = 'application/json'
    return headers
  }

  async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const { json, headers, ...rest } = options
    const response = await fetch(path, {
      // Canonical documents can be rebuilt at the same URL. Always validate a
      // browser-held response; origin/edge ETags still make unchanged reads
      // cheap, while an old max-age entry can no longer mask a completed refresh.
      cache: rest.method && rest.method !== 'GET' ? 'no-store' : 'no-cache',
      ...rest,
      headers: { ...buildHeaders(json), ...(headers as Record<string, string>) },
    })
    const contentType = response.headers.get('content-type') || ''
    const body = contentType.includes('json') ? await response.json() : await response.text()
    if (!response.ok) throw new Error(messageFrom(body, response))
    return body as T
  }

  // ---- Library reads -------------------------------------------------------

  function search(query: string, kind = '', limit = 30): Promise<{ results: EntitySummary[] }> {
    const params = new URLSearchParams({ q: query, limit: String(limit) })
    if (kind) params.set('kind', kind)
    return request(`${BASE}/search?${params}`)
  }

  function browse(options: { kind?: string; sort?: string; offset?: number; limit?: number; q?: string } = {}): Promise<BrowseResult> {
    const params = new URLSearchParams()
    params.set('limit', String(options.limit ?? 24))
    params.set('offset', String(options.offset ?? 0))
    params.set('sort', options.sort || 'updated')
    if (options.kind) params.set('kind', options.kind)
    if (options.q) params.set('q', options.q)
    return request(`${BASE}/browse?${params}`)
  }

  function latest(kind: string, limit = 12): Promise<BrowseResult> {
    const params = new URLSearchParams({ limit: String(limit) })
    if (kind) params.set('kind', kind)
    return request(`${BASE}/latest?${params}`)
  }

  function stats(): Promise<LibraryStats> {
    return request(`${BASE}/stats`)
  }

  function collections(): Promise<{ collections: CollectionCard[] }> {
    return request(`${BASE}/collections`)
  }

  function collection(id: string): Promise<CollectionCard> {
    return request(`${BASE}/collections/${encodeURIComponent(id)}`)
  }

  function entity(id: string): Promise<EntityDocument> {
    const params = localeState.query()
    return request(`${BASE}/entities/${id}?${params}`)
  }

  function entityImages(id: string, limit = 100): Promise<ImagesResponse> {
    const params = localeState.query()
    params.set('limit', String(limit))
    return request(`${BASE}/entities/${id}/images?${params}`)
  }

  function entityCredits(id: string): Promise<{ results: Credit[] }> {
    return request(`${BASE}/entities/${id}/credits`)
  }

  function topTracks(artistId: string, options: { offset?: number; limit?: number } = {}): Promise<TopTracksResponse> {
    const params = new URLSearchParams()
    params.set('offset', String(options.offset ?? 0))
    params.set('limit', String(Math.min(options.limit ?? 50, 100)))
    return request(`${BASE}/entities/${encodeURIComponent(artistId)}/top-tracks?${params}`)
  }

  function entityRelations(id: string, options: { type?: string; limit?: number; offset?: number } = {}): Promise<RelationsResponse> {
    const params = new URLSearchParams()
    if (options.type) params.set('type', options.type)
    params.set('limit', String(options.limit ?? 100))
    params.set('offset', String(options.offset ?? 0))
    return request(`${BASE}/entities/${id}/relations?${params}`)
  }

  async function allEntityRelations(id: string, type?: string): Promise<RelationsResponse> {
    const limit = 100
    const relations: RelationsResponse['relations'] = []
    let offset = 0
    let total = 0
    do {
      const page = await entityRelations(id, { type, limit, offset })
      const items = page.relations ?? []
      relations.push(...items)
      total = page.total ?? relations.length
      offset += items.length
      if (!items.length) break
    } while (offset < total)
    return { relations, total, offset: 0, limit: relations.length }
  }

  function recordingFingerprints(id: string): Promise<{ recording_id: string; items: any[] }> {
    return request(`${BASE}/recordings/${id}/fingerprints`)
  }

  function recordingLyrics(id: string): Promise<{ recording_id: string; items: LyricDocument[] }> {
    return request(`${BASE}/recordings/${id}/lyrics`)
  }

  function person(id: string): Promise<PersonDocument> {
    return request(`${BASE}/persons/${id}`)
  }

  // Canonical filmography by Heya person id (the provider-scoped route is gone).
  function personCredits(personEntityId: string, options: { offset?: number; limit?: number } = {}): Promise<PersonCreditsResponse> {
    const params = new URLSearchParams()
    params.set('offset', String(options.offset ?? 0))
    params.set('limit', String(options.limit ?? 100))
    return request(`${BASE}/persons/${encodeURIComponent(personEntityId)}/credits?${params}`)
  }

  // Fetches the full filmography across pages. Detail-page scale (hundreds, not
  // thousands), so a bounded accumulation is fine.
  async function allPersonCredits(personEntityId: string): Promise<PersonCreditsResponse> {
    const limit = 100
    const credits: PersonCreditsResponse['credits'] = []
    let offset = 0
    let total = 0
    let person: PersonCreditsResponse['person'] = {}
    do {
      const page = await personCredits(personEntityId, { offset, limit })
      person = page.person ?? person
      const items = page.credits ?? []
      credits.push(...items)
      total = page.total ?? credits.length
      offset += items.length
      if (!items.length) break
    } while (offset < total)
    return { person, credits, total }
  }

  function season(id: string): Promise<SeasonResource> {
    return request(`${BASE}/seasons/${id}`)
  }

  function episode(id: string): Promise<EpisodeResource> {
    return request(`${BASE}/episodes/${id}`)
  }

  function health(): Promise<{ status?: string }> {
    return request(`${BASE}/health/ready`)
  }

  // Admin-only: River job queue introspection. The session cookie authorises.
  function adminJobs(options: { state?: string; kind?: string; limit?: number } = {}): Promise<AdminJobsResponse> {
    const params = new URLSearchParams()
    if (options.state) params.set('state', options.state)
    if (options.kind) params.set('kind', options.kind)
    params.set('limit', String(options.limit ?? 50))
    return request(`${BASE}/admin/jobs?${params}`)
  }

  function adminJobAction(action: AdminJobAction): Promise<AdminJobActionResult> {
    return request(`${BASE}/admin/jobs/actions`, { method: 'POST', json: true, body: JSON.stringify({ action }) })
  }

  // ---- Discovery / resolution workflow ------------------------------------

  function createDiscovery(body: Record<string, unknown>): Promise<any> {
    return request(`${BASE}/discoveries`, {
      method: 'POST',
      json: true,
      headers: { Prefer: 'wait=5' },
      body: JSON.stringify(body),
    })
  }

  function getDiscovery(id: string): Promise<any> {
    return request(`${BASE}/discoveries/${id}`)
  }

  async function pollDiscovery(id: string, attempts = 90): Promise<any> {
    for (let attempt = 0; attempt < attempts; attempt++) {
      const current = await getDiscovery(id)
      if (current.state === 'completed' || current.state === 'failed') return current
      await sleep(Math.min(350 + attempt * 35, 1200))
    }
    throw new Error('Discovery is still running. Try again in a moment.')
  }

  function createResolution(body: Record<string, unknown>): Promise<any> {
    return request(`${BASE}/resolutions`, {
      method: 'POST',
      json: true,
      headers: { Prefer: 'wait=5' },
      body: JSON.stringify(body),
    })
  }

  function getJob(id: number | string): Promise<any> {
    return request(`${BASE}/jobs/${id}`)
  }

  async function pollJob(id: number | string, attempts = 120): Promise<any> {
    for (let attempt = 0; attempt < attempts; attempt++) {
      const job = await getJob(id)
      // A materialized entity is success regardless of the exact River state.
      if (job.entity_id) return job
      if (job.state === 'completed') return job
      if (['cancelled', 'discarded', 'failed'].includes(job.state)) {
        throw new Error(job.error || `Job ${job.state}`)
      }
      await sleep(Math.min(450 + attempt * 30, 1500))
    }
    throw new Error('The ingestion job is still running. Try again in a moment.')
  }

  function refreshEntity(id: string): Promise<any> {
    return request(`${BASE}/entities/${id}/refreshes`, { method: 'POST' })
  }

  return {
    request,
    search,
    browse,
    latest,
    stats,
    collections,
    collection,
    entity,
    entityImages,
    entityCredits,
    topTracks,
    entityRelations,
    allEntityRelations,
    recordingFingerprints,
    recordingLyrics,
    person,
    personCredits,
    allPersonCredits,
    season,
    episode,
    health,
    adminJobs,
    adminJobAction,
    createDiscovery,
    getDiscovery,
    pollDiscovery,
    createResolution,
    getJob,
    pollJob,
    refreshEntity,
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms))
}
