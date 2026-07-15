# HeyaMedia → HeyaMetadata v2 integration handoff

This is the implementation brief for replacing HeyaMedia's in-process metadata
search, provider fan-out, enrichment, identity, and metadata storage with the
HeyaMetadata v2 service.

It describes the contract as implemented in this repository. The machine
contract remains authoritative:

- interactive API reference: `GET /api/docs` (Scalar)
- live OpenAPI 3.1 document: `GET /api/openapi.json` or
  `GET /api/openapi.yaml`
- live OpenAPI 3.0 document: `GET /api/openapi-3.0.json` or
  `GET /api/openapi-3.0.yaml`
- committed OpenAPI snapshot: `api/openapi.yaml`
- generated Go client: `sdk/go/heyametadata/client.gen.go`
- architectural source of truth: `HeyaMetadataV2.md`
- semantic product contract: `coverage/*.json`
- lower-level identity-flow notes: `docs/client-resolution-flow.md`

When this document and the OpenAPI document disagree, use OpenAPI for HTTP
shape and status codes, then verify behavior against the live handler. Update
this document if the difference is intentional.

## Outcome and ownership boundary

After the migration:

- HeyaMetadata owns metadata discovery, canonical identity, upstream provider
  access, reconciliation, enrichment, freshness, artwork materialization, and
  the combined metadata document.
- HeyaMedia stores and passes around the Heya canonical entity ID. It does not
  build a second combined metadata document and does not select one provider as
  a competing source of truth.
- Provider IDs remain useful as evidence and migration inputs. They are not the
  identity exposed to the rest of HeyaMedia.
- HeyaMedia may cache returned documents, but `projection_version` and the
  public change feed govern invalidation. A cached provider response must never
  become a second canonical record.
- HeyaMedia should not retain its old Mongo/Meilisearch enrichment pipeline once
  all call sites have moved. Media-server features that are not metadata, such
  as community skip segments, remain in HeyaMedia until they have a deliberate
  replacement.

The one durable foreign key HeyaMedia needs is conceptually:

```text
heya_metadata_entity_id UUID
heya_metadata_kind      TEXT
```

Keeping `projection_version` and the last consumed change cursor locally is
also useful. External IDs may be retained as matching hints or diagnostics, but
not as the primary key.

## Base URL and development topology

`make dev` in HeyaMetadata exposes the stable development proxy at:

```text
http://localhost:3030
```

The Air-managed Go API and Nuxt process live behind it on ports 3031 and 3032.
A client should use port 3030, not either implementation port.

If HeyaMedia and HeyaMetadata run side by side, move HeyaMedia to another host
port (for example 3040) or use container service discovery. Do not start two
processes that both expect localhost:3030.

Configure the base URL in HeyaMedia; do not compile it into handlers:

```text
HEYA_METADATA_URL=http://localhost:3030
```

Use `GET /api/v2/health/ready`, not liveness, before sending normal work.

## The identity state machine

There are deliberately three different operations:

1. **Search** reads the local canonical index for query-only matching. It is a
   fast path and never calls an upstream provider.
2. **Discovery** accepts every external identifier and local fact HeyaMedia
   has. HeyaMetadata resolves known claims locally, internally crosswalks a
   fresh identifier into the canonical pipeline, or searches upstream by hints.
3. **Resolution** selects an opaque HeyaMetadata candidate reference. The
   client never sees or reconstructs the provider identity behind it.

Resolution is an optional ambiguity branch, not a mandatory stage after every
discovery. A discovery that returns `result.entity_id` is already resolved.

Every successful path ends with `GET /api/v2/entities/{id}`.

```text
identifiers present?
  |
  +-- yes -> POST /api/v2/discoveries with every identifier + facts
  |
  `-- no  -> GET /api/v2/search?q=...&kind=...
              |
              +-- acceptable result -> persist result.id
              |
              `-- no acceptable result -> POST /api/v2/discoveries with query + facts

completed discovery
  |
  +-- result.entity_id
  |     `-- persist/read that Heya entity directly
  |
  `-- result.status = needs_selection
        `-- choose candidate using evidence
              `-- POST {candidate_ref} to /api/v2/resolutions
                    |
                    +-- 200 completed -> persist response.entity_id
                    |
                    `-- 202 accepted -> poll GET /api/v2/jobs/{job.id}
                                              `-- persist job.entity_id

GET /api/v2/entities/{persisted Heya ID}
```

Do not combine these into an old-style endpoint that always fans out upstream.
That would remove the fast local path, make latency unpredictable, and waste
provider quotas.

### Recommended client algorithm

```text
Resolve(kind, query, identifiers, hints, providerCredentials):
    if identifiers is empty:
        local = GET /api/v2/search?q=query&kind=kind
        if an acceptable canonical summary can be selected:
            return GET /api/v2/entities/{local.id}

    discovery = POST /api/v2/discoveries with
                {kind, query?, identifiers: allKnownIDs, hints}
    while discovery.state is queued or working:
        discovery = GET /api/v2/discoveries/{discovery.id}

    if discovery.state is failed:
        return an upstream-discovery error

    if discovery.result.entity_id is not empty:
        id = discovery.result.entity_id
        persist id and kind
        return GET /api/v2/entities/{id}

    candidate = apply product selection policy to discovery.result.candidates
    if no candidate can be selected safely:
        return candidates to the caller/user for disambiguation

    resolution = POST /api/v2/resolutions with
                 {candidate_ref: candidate.candidate_ref}
    while resolution is accepted and its job has no entity_id:
        job = GET /api/v2/jobs/{resolution.job.id}
    id = resolution.entity_id or job.entity_id

    persist id and kind
    return GET /api/v2/entities/{id}
```

Preserve discovery IDs and River job IDs in durable request state if the
calling workflow itself is durable. Retrying the same discovery or resolution
after an HTTP timeout is safe; inventing a second local identity is not.

Use bounded exponential polling with jitter. A reasonable client starts around
200 ms, caps around 2 seconds, and applies an overall workflow deadline. Honor
`Retry-After` whenever present. Never poll in a tight loop.

## Step 1: query-only local canonical search

```http
GET /api/v2/search?q=ano&kind=artist&limit=20
Accept-Language: en,ja;q=0.9
```

Supported query parameters:

| Parameter | Meaning |
| --- | --- |
| `q` | Required title, name, or alias query |
| `kind` | Strongly recommended canonical kind filter |
| `limit` | 1–100; default 20 |
| `year` | Exact release/start year |
| `genre` | Exact normalized genre |
| `country` | Country filter |
| `language` | Language filter |
| `status` | Lifecycle/status filter |

When the caller has any external identifiers, skip this search step and submit
all of them to discovery. That is required even if one identifier is likely
known locally, because discovery verifies fresh and known evidence together.
Search remains the query-only fast path and never calls an upstream service.

Search returns polymorphic summary documents in `results`. The canonical ID is
`result.id`, not `result.entity_id`. Every summary has the stable core:

```json
{
  "schema_version": 1,
  "projection_version": 7,
  "id": "050aa960-...",
  "kind": "movie",
  "slug": "the-matrix-1999",
  "display": {
    "title": "The Matrix",
    "year": 1999,
    "image_id": "..."
  },
  "freshness": { "state": "fresh" }
}
```

Artist and author displays use `display.name`; most other domains use
`display.title`. Person documents currently use `display.title`. The adapter
should have one display-label helper instead of stringifying arbitrary objects:

```text
label = display.name ?? display.title ?? display.original_title ?? "Unknown"
```

This is also the rule that prevents UI output such as `[object Object]`.

A fuzzy name hit is not automatically a durable identity match. Use kind,
year/date, aliases, credited artists/authors, country/language, and existing
facts to decide whether the local row is acceptable. If doubt remains, run
discovery with structured hints.

## Step 2: provider-transparent discovery

The generic operation is:

```http
POST /api/v2/discoveries
Content-Type: application/json
Prefer: wait=5

{
  "kind": "artist",
  "query": "ano",
  "identifiers": [
    {"scheme": "musicbrainz", "value": "ebb4513e-4aab-4ac9-a949-14e77bb7b836"}
  ],
  "limit": 10,
  "hints": {
    "country": "JP",
    "type": "person",
    "aliases": ["あの"],
    "releases": [
      {
        "title": "猫猫吐吐",
        "year": 2023,
        "identifiers": [
          {"scheme": "apple", "value": "..."},
          {"scheme": "deezer", "value": "..."}
        ]
      }
    ]
  }
}
```

The default inline wait is 1.2 seconds. `Prefer: wait=N` waits for at most five
seconds. `Prefer: respond-async` returns immediately. The response is:

- `200` with `state: completed` if work finished during the wait;
- `202` with `state: queued` or `working` if it remains asynchronous;
- `503` with `Retry-After` if all worker attempts fail; the same normalized
  request may be submitted again safely;
- `GET /api/v2/discoveries/{id}` returns the durable resource afterward.

Discovery resource states are `queued`, `working`, `completed`, and `failed`.
Equivalent normalized requests reuse durable work and completed results for six
hours. The resource exposes `expires_at`.

When identifiers uniquely establish an entity, the completed result contains
`entity_id` directly. No resolution call is needed. It also reports
`identifier_evidence` outcomes (`resolved`, `corroborating`, `unused`,
`unsupported`, or `conflict`) so HeyaMedia can diagnose what contributed
without learning provider routing.

Identifier evidence is processed before fuzzy title/name search. If one
identifier is already known locally while another supported identifier is
fresh, discovery continues in the durable worker instead of returning the
known entity early. The worker crosswalks the fresh evidence: agreement returns
the same Heya UUID with corroborating evidence; disagreement returns opaque
reviewable candidates. HeyaMedia must not discard fresh IDs merely because one
of the submitted IDs already has a local claim.

When selection is required, the completed result contains:

- `recommendation`: `strong_match`, `likely_match`, `ambiguous`, or `no_match`;
- ranked `candidates`;
- a 0–1 `confidence` and per-field `evidence` on every candidate;
- `match`: `strong`, `likely`, `possible`, or `weak`;
- an opaque `candidate_ref` on each candidate.

Candidates deliberately do not contain `provider`, `namespace`, `value`,
`existing_entity_id`, or a provider-shaped `resolution` object.

A decisively ranked query-and-hint candidate that already has an accepted
external-ID claim completes directly with the existing `entity_id`. This is
provider-claim reconciliation, never a title/year merge. Weak or ambiguous
same-title productions remain opaque candidates requiring selection.

The current generic recommendation policy requires both confidence and margin
over the second result. Do not replace it with “rank 1 always wins.” A safe
initial HeyaMedia policy is:

- auto-select `strong_match`;
- auto-select `likely_match` only when the calling scanner supplied multiple
  corroborating hints and product policy explicitly allows it;
- surface `ambiguous` choices or retry with better hints;
- never auto-select `no_match` or a merely weak first row.

Always display the candidate evidence when asking a user to choose. It exists
specifically to make namesakes such as `ano`, `Ano`, and other similarly named
artists resolvable without guessing.

### Supported discovery kinds and identifier evidence

HeyaMedia may submit all identifiers it has. Common bootstrap evidence is:

| Kind | Accepted fresh identifier evidence |
| --- | --- |
| `movie` | TMDB, IMDb |
| `artist` | MusicBrainz, Apple/iTunes artist ID, Deezer artist ID |
| `release_group` | MusicBrainz |
| `recording` | MusicBrainz |
| `musical_work` | Open Opus |
| `tv_show` | TVMaze, TVDB, IMDb, TMDB |
| `anime` | AniDB, MyAnimeList, AniList, TVDB, TMDB |
| `book_work`, `manga_volume`, `comic_volume` | Open Library work/edition, ISBN |
| `manga` | Kitsu; already-known AniList/MyAnimeList claims also resolve locally |

This table describes input evidence, not provider-selection rules. Its contents
may grow without a HeyaMedia change because HeyaMetadata owns crosswalking and
all private ingestion strategy.

There is no direct upstream discovery operation for issued `release`, edition,
`author`, or `person` entities. Issued releases come from release-group edition
relationships; editions and authors come from publication ingestion; people
come from title credits. Once known, all of them are searchable and readable.

Dedicated convenience routes inject the kind but use the same discovery
resource:

| Operation | Injected kind |
| --- | --- |
| `POST /api/v2/tv/discoveries` | `tv_show` |
| `POST /api/v2/anime/discoveries` | `anime` |
| `POST /api/v2/manga/discoveries` | `manga` |
| `POST /api/v2/manga/volumes/discoveries` | `manga_volume` |
| `POST /api/v2/comics/discoveries` | `comic_volume` |

For a generic HeyaMedia adapter, prefer the generic route so one state machine
handles every supported kind.

### Useful hints by domain

All hint fields are optional. Send facts the scanner genuinely knows; do not
manufacture empty or guessed values.

| Domain | High-value hints |
| --- | --- |
| Movie | `year`, `date`, `original_title`, `aliases`, `language`, `country` |
| Artist | `country`, `area`, `type`, `begin_date`, `end_date`, `aliases`, `releases[]` |
| Release group | `year`, `date`, `type`, `artists`, `tracks` |
| Recording | `artists`, `duration_ms`, exact `isrcs`, `releases[]` |
| Musical work | `composers`, `catalogue`, `year` |
| TV | `year`, `country`, `language`, `network`, `status`, `episodes[]` |
| Anime | `year`, `type`, `episode_count`, `source`, `studios`, `episodes[]` |
| Books/publications | `year`, `authors`, exact `isbns` |

`episodes[]` contains `{title, season, number}`. `releases[]` contains
`{title, year, type}`. Release titles, track titles, ISRCs, durations, and IDs
are evidence—not individually infallible identities.

## Step 3: opaque selection and ingestion, only when required

Skip this entire step when discovery returns `result.entity_id`. Use
`/api/v2/resolutions` only for a safely selected opaque candidate from a
`needs_selection` result.

Pass only the selected candidate reference:

```http
POST /api/v2/resolutions
Content-Type: application/json

{
  "candidate_ref": "7edb3b9e-0ae8-4ac8-8735-37046f67aa2d"
}
```

If the candidate is already canonical, the response is `200`:

```json
{
  "state": "completed",
  "entity_id": "...",
  "entity": {"id": "...", "kind": "artist"}
}
```

If ingestion is needed, the response is `202`:

```json
{
  "state": "accepted",
  "job": {
    "id": 1234,
    "kind": "artist_ingest",
    "state": "available"
  }
}
```

Poll `GET /api/v2/jobs/{id}`. The operation is successful when `entity_id` is
present. A populated `error`, or a terminal cancelled/discarded job without an
entity ID, is a failed resolution. River's intermediate state vocabulary is an
implementation detail; do not make success depend on one exact intermediate
string.

Resolution is idempotent at the private identity/job and canonical-entity
level. It is safe to retry after network timeouts. A candidate reference is
short-lived selection state, not an identity to persist.

Identifiers that cannot currently bootstrap an entity are returned as unused
or unsupported evidence without breaking otherwise valid discovery. HeyaMedia
must not compensate by adding provider-specific routing. For existing data,
send the media kind, every old external ID as `{scheme,value}`, and all useful
title/name hints to discovery. HeyaMetadata decides which evidence can resolve
locally, crosswalk, or bootstrap ingestion. Resolve only by a returned opaque
`candidate_ref` when the result needs selection.

## Step 4: canonical reads

The universal read is:

```http
GET /api/v2/entities/{heya-uuid}?language=en&fallback_languages=ja
Accept-Language: en-GB,en;q=0.9,ja;q=0.8
```

The service follows entity redirects internally, so a retired/merged ID reads
the surviving canonical entity. HeyaMedia should update its stored ID when the
returned document's `id` differs from the requested ID.

Canonical detail documents share this conceptual envelope:

```go
type CanonicalDocument struct {
    SchemaVersion     int64                    `json:"schema_version"`
    ProjectionVersion int64                    `json:"projection_version"`
    ID                string                   `json:"id"`
    Kind              string                   `json:"kind"`
    Slug              string                   `json:"slug"`
    Display           json.RawMessage          `json:"display"`
    ExternalIDs       []ExternalID             `json:"external_ids"`
    Data              json.RawMessage          `json:"data"`
    Freshness         json.RawMessage          `json:"freshness"`
    Provenance        map[string]json.RawMessage `json:"provenance,omitempty"`
}
```

The precise `display` and `data` fields vary by kind. Do not flatten the new
documents back into the old `TitleDoc`; doing so would discard the domain
separation, typed external IDs, provenance, language information, editions,
recordings, and future provider evidence.

Common rules:

- render `display`, which is localized for the request;
- treat `external_ids` as `{provider, namespace, value}` tuples, never as an
  unqualified string map;
- use `display.image_id` and image endpoints instead of upstream URLs;
- use `projection_version` for cache/version comparisons;
- preserve `schema_version` in any durable serialized copy;
- consume `data` with a kind-specific model;
- do not present `provenance` as metadata content, but keep it available for
  diagnostics and “where did this come from?” UI.

An entity read may return `freshness.state: stale` while also returning usable
data and enqueuing a low-priority refresh. Use the document. Do not restart
discovery just because it is stale.

### Domain access matrix

| Kind | Canonical read | Additional interfaces |
| --- | --- | --- |
| `movie` | `/api/v2/entities/{id}` | credits, ratings, images; collection and recommendations in `data` |
| `artist` | `/api/v2/entities/{id}` | `relations?type=discography`, `top-tracks`, images; similar artists in `data` |
| `release_group` | `/api/v2/entities/{id}` | `relations?type=editions`, images |
| `release` | `/api/v2/releases/{id}` or generic entity read | ordered media/tracks; each resolved track has `recording_entity_id` |
| `recording` | `/api/v2/recordings/{id}` or generic entity read | fingerprints and lyrics endpoints |
| `musical_work` | `/api/v2/entities/{id}` | composer relation |
| `tv_show` | `/api/v2/tv/shows/{id}` or generic entity read | credits, ratings, images, embedded season/episode IDs |
| `anime` | `/api/v2/anime/{id}` or generic entity read | credits, ratings, images, embedded season/episode IDs |
| `book_work`, `book_edition` | `/api/v2/entities/{id}` | editions/authors/series in canonical publication data, images |
| `manga` | `/api/v2/manga/{id}` or generic entity read | images |
| `manga_volume` | `/api/v2/manga/volumes/{id}` or generic entity read | edition data, images |
| `manga_edition` | `/api/v2/entities/{id}` | images |
| `comic_volume` | `/api/v2/comics/volumes/{id}` or generic entity read | edition data, images |
| `comic_edition` | `/api/v2/entities/{id}` | images |
| `author` | `/api/v2/entities/{id}` | author data created through publication ingestion |
| `person` | `/api/v2/persons/{id}` or generic entity read | combined filmography |

The generic entity route is the best default for HeyaMedia because it also
accepts localization and request-scoped provider credentials. Dedicated routes
are useful where their typed resource or shareable URL is important.

### Canonical relation IDs

Every independently navigable object is represented by a Heya UUID. This
includes movies, shows, seasons, episodes, artists, release groups, issued
releases, track placements, recordings, books/editions, authors, manga/comic
volumes, people, and movie collections. Important nested fields are:

- `person_entity_id` on every cast/crew credit;
- `artist_entity_id` on materialized music credits;
- track-placement `id` plus `recording_entity_id` on an issued release;
- `entity_id` on materialized release-group editions, recommendations,
  collection members, filmography items, and generic relation targets;
- `release_entity_id` and `release_group_entity_id` on recording appearances;
- season/episode `id` and episode `season_id`;
- publication author/edition `id` and edition `work_id`.

When an upstream source reports a related object that is not yet canonical,
the relation omits the target UUID and exposes `resolution_state: unresolved`.
Clients may display that evidence but must not navigate using its passive
provider ID. `resolution_state: materialized` means the Heya target ID is safe
to retain and follow.

## Relationships, credits, ratings, and recommendations

### Generic relationships

```http
GET /api/v2/entities/{id}/relations?type=discography&offset=0&limit=50
```

Each relation contains a Heya target when materialized and passive provider
provenance:

```json
{
  "relation_type": "discography",
  "target_kind": "release_group",
  "target_entity_id": "...",
  "provider": "itunes",
  "namespace": "collection",
  "provider_value": "123",
  "resolution_state": "materialized",
  "metadata": {"title": "..."},
  "target": {"id": "...", "display": {"title": "..."}}
}
```

Use `target_entity_id` when populated. `target` is the compact canonical search
summary when available. Provider-only relations are evidence or deferred work;
they are not safe canonical links.

Current high-value relation types are:

- artist → `discography` → release groups;
- release group → `editions` → issued releases;
- musical work → `composer` → artist/person identity.

Artist discography is reconciled from MusicBrainz, Discogs, Deezer,
Apple/iTunes storefront evidence, and other configured music providers. The
caller must not append provider album arrays itself. HeyaMetadata performs
romanization-aware deduplication, track/date evidence reconciliation, and
promotion to canonical release groups.

MusicBrainz is not required to create an artist. A direct Apple/iTunes or
Deezer artist identifier can establish the canonical Heya artist and its public
discography. Include known Apple/Deezer album IDs in the matching
`hints.releases[].identifiers` array; HeyaMetadata validates the fetched release
credit and uses the ID as catalog evidence. MusicBrainz release/release-group,
Apple album, Deezer album, and Discogs release/master IDs in the same array are
also resolved privately to their credited artist or artists. Release identity
evidence is checked even when the top-level artist ID is already known. This
means a correct release can corroborate an artist identifier—or expose a stale
storefront artist identifier without requiring the client to understand either
provider.

Submit every available artist identifier in one discovery request. HeyaMetadata
ingests and crosswalks each supported root, returning a canonical `entity_id`
only when the roots converge on one Heya artist. Roots that resolve to different
artists produce opaque, reviewable candidates. An authoritative MusicBrainz
artist may consolidate duplicate Apple, Deezer, or Discogs roots only when its
explicit URL relationships link those roots, their primary names agree, and no
different MusicBrainz artist claim exists. A shared or similar name is never
sufficient to merge artists. During refresh, a provider record whose direct
identity belongs to another active Heya artist is quarantined rather than mixed
into the current artist's names, artwork, popularity, or provenance.

Wikidata Q-IDs are not artist identities. One Wikidata item may intentionally
list several distinct MusicBrainz stage names or projects. HeyaMetadata retains
Wikidata descriptions, links, and artwork only when an item label exactly
matches the primary MusicBrainz artist name; the Q-ID never creates or merges a
canonical artist claim.

Common credit joins (`feat.`,
`featuring`, `ft.`, `f/`, `with`/`w/`, `vs.`, `presents`, `meets`, `&`, `and`,
`x`, `×`, `/`, `;`, and spaced `:`) are parsed only after literal artist
resolution fails and never merge artists by name.

### Artist top tracks

```http
GET /api/v2/entities/{artist-id}/top-tracks?offset=0&limit=50
```

This is the bounded canonical interface for an artist's “Popular Tracks” rail.
Results preserve Last.fm rank, title, playcount, listener count, URL, and
MusicBrainz recording evidence. `recording_entity_id` is populated when that
recording already exists; unresolved rows remain useful evidence and do not
create speculative recording identities. `sources[]` reports observation time,
upstream total, projection version, and whether the provider's top-100 snapshot
was truncated. Artist detail does not embed the unbounded ranking.

Last.fm occasionally returns the correct name-scoped artist page with an
incorrect MusicBrainz artist ID. HeyaMetadata retains those popularity rows
only when the returned artist name exactly matches a canonical name or alias.
In that fallback mode `recording_mbid` and `recording_entity_id` remain absent,
so useful rankings cannot create cross-artist identity contamination.

### Issued releases and track links

An artist's `discography` points to release groups. A release group's
`editions` point to issued releases. `GET /api/v2/releases/{id}` returns ordered
media and track placements. A canonicalized track includes
`recording_entity_id`; use it to link to `GET /api/v2/recordings/{id}`. It also
includes `lyrics_available`, populated in one batch by HeyaMetadata, so clients
can decide whether to fetch `/recordings/{id}/lyrics` without an N+1 probe.

This distinction is intentional:

```text
artist → release_group (the conceptual album/single)
       → release (country/date/label/barcode edition)
       → track placement
       → recording (the performed audio identity)
```

Do not collapse release groups, releases, and recordings into one old “album”
or “track” ID space.

### Credits and people

```http
GET /api/v2/entities/{id}/credits?credit_type=cast&offset=0&limit=5000
GET /api/v2/entities/{id}/credits?credit_type=crew&offset=0&limit=5000
```

Credits apply to movie, TV, and anime entities. Every projected credit carries
the canonical `person_entity_id`. The endpoint retains `total`, `offset`, and
`limit` pagination while permitting pages of up to 5,000 credits, so unusually
large shows do not require a long chain of sequential requests. Link to:

```http
GET /api/v2/persons/{person_entity_id}
```

The reverse filmography index is canonical-ID-only:

```http
GET /api/v2/persons/{person_entity_id}/credits
```

It returns known canonical filmography entries with `entity_id`. Provider-only
filmography observations remain explicitly unresolved; HeyaMedia never chooses
a provider-person route.

### Ratings

```http
GET /api/v2/entities/{id}/ratings?offset=0&limit=100
```

Ratings preserve each provider's native scale, vote count, and provenance. Do
not average or coerce them into a single 0–10 score in the transport adapter.
Presentation code may normalize a copy if it clearly labels the source.

### Recommendations and similar entities

Movie recommendations are embedded in `data.recommendations`. Use
`recommendation.entity_id` when present. Artist similarity evidence is embedded
in `data.similar_artists`; it may remain provider-only if the target has not
been canonicalized. Do not create an entity merely to make every recommendation
clickable.

## TV, anime, seasons, and episodes

TV and anime are separate canonical kinds. Do not map both back to the old `tv`
kind:

```text
tv_show = conventional television with canonical Heya show/season/episode UUIDs
anime   = dedicated anime domain with canonical Heya title/season/episode UUIDs
```

Provider selection, crosswalking, reconciliation, and merge priority are
private HeyaMetadata behavior. HeyaMedia follows only canonical UUIDs and must
not reproduce source-precedence rules.

Show documents include canonical UUIDs on their season and episode entries.
Those IDs have standalone, bookmarkable reads:

```http
GET /api/v2/seasons/{season-uuid}
GET /api/v2/episodes/{episode-uuid}
```

A season resource returns its parent show, localized titles/overviews, dates,
status, counts, typed external IDs, opaque images, ordered episode UUIDs, and
the ordered episode resources. An episode resource returns its parent show,
season UUID, localized text, typed external IDs, ratings, opaque stills,
explicit special/type state, and scheme-aware numbers. Do not assume one
provider's ordering is universally canonical.

Number schemes are normalized lowercase. `aired` is the canonical presentation
selected by HeyaMetadata when sufficient evidence exists; source-specific
schemes remain passive provenance, and anime may additionally expose integer
`absolute` order. Fractional source numbers remain decimals. HeyaMedia must not
derive identity or season association from number-array order or apply its own
source precedence. Specials are explicit and season zero is retained.

## Language-aware presentation and artwork

For canonical detail reads, preference order is:

1. explicit `language` query;
2. ordered comma-separated `fallback_languages` query;
3. `Accept-Language` header;
4. neutral/provider fallback.

`country=XX` adds presentation-region preference. Search, browse, and latest
use `Accept-Language` for compact summaries.

Always render the localized `display` chosen by the server. Native-script names
and titles remain in the detailed title/name arrays and aliases; the client
should not independently transliterate the canonical display value.

Artwork selection is a separate language-aware interface:

```http
GET /api/v2/entities/{id}/images?class=poster&language=en-GB&fallback_languages=en,ja&country=GB&limit=25
Accept-Language: en-GB,en;q=0.9,ja;q=0.8
```

The response contains:

- `language_preferences`: normalized order used by the ranker;
- `selections`: selected image ID keyed by artwork class;
- `results`: ranked candidates with class, language, country, dimensions,
  provider, materialization state, and selection reason.

Use `selections[class]`; do not reproduce server ranking or take the first
provider image. Common classes include `poster`, `backdrop`, `logo`,
`clearlogo`, `banner`, `cover`, `back_cover`, `profile`, `thumb`, `clearart`,
`characterart`, `icon`, `cinemagraph`, `disc`, `booklet`, `spine`, and `obi`.
The set is intentionally extensible; clients should render unfamiliar classes
in a generic artwork group instead of discarding them.

Read image bytes through HeyaMetadata:

```http
GET /api/v2/images/{image-id}
GET /api/v2/images/{image-id}/variants/webp/640
GET /api/v2/images/{image-id}/variants/webp/1280
```

The first request may return `202 application/json` with a materialization job
and `Retry-After: 2`. Poll the job or wait, then retry the image URL. A ready
response is image bytes with an ETag, dimensions, and long-lived cache headers.
Never hand HeyaMedia an arbitrary upstream URL to proxy.

## Music fingerprints and lyrics

Canonical recording evidence is available through:

```http
GET /api/v2/recordings/{id}/fingerprints
GET /api/v2/recordings/{id}/lyrics
```

Fingerprints identify their algorithm, version, encoding, duration, source,
and checksum. Lyrics retain provider evidence and may contain plain and/or
synchronized text. Each lyric item includes `scope` and
`source_recording_id`. Scope `recording` is exact evidence for the requested
recording. Scope `musical_work` is untimed plain text inherited through an
explicit shared-work relationship; synchronized timestamps are never
inherited across recordings with different timing.

Album and issued-release tracklists include a compact `lyrics_available`
boolean. Fetch full lyrics only for tracks where that flag is true.

A client-generated Chromaprint can be matched without exposing a broad
mutation surface:

```http
POST /api/v2/fingerprint-matches
Prefer: wait=5
X-Heya-AcoustID-API-Key: <optional user key>
Content-Type: application/json

{
  "encoding": "base64-uint32le+acoustid",
  "raw_fingerprint": "...",
  "acoustid_fingerprint": "...",
  "duration_ms": 213000
}
```

Poll `GET /api/v2/fingerprint-matches/{id}`, not merely the nested River job.
The result contains ranked recording candidates. A materialized match carries
`entity_id`; an upstream-only match carries an opaque `candidate_ref` accepted
by `/api/v2/resolutions`. It never returns an MBID or provider-shaped resolution
for client routing. Submitted fingerprint payloads expire after one hour and
are erased after completion.

## Freshness, refresh, caching, and change consumption

Normal reads implement stale-while-revalidate. That should be the default
refresh mechanism. The explicit operation:

```http
POST /api/v2/entities/{id}/refreshes
```

queues an interactive refresh and returns `202` with a River job. Use it for an
explicit user/operator action, not on every playback or page load. Recordings
are refreshed internally and intentionally do not support manual refresh.

HeyaMetadata tracks access frequency and schedules hot entities more often than
cold entities. HeyaMedia does not need its old metadata auto-refresher.

For durable cache/index synchronization, consume:

```http
GET /api/v2/changes?after={cursor}&limit=500&stream_id={last-seen-stream-id}
```

The initial request may omit `stream_id`. Every successful page returns:

```json
{
  "stream_id": "6e53b69c-d158-46a5-913d-6e4a5401bcf8",
  "head_cursor": 32554,
  "entries": [],
  "next_cursor": 32554
}
```

Persist `stream_id` beside the cursor and include it on subsequent requests.
`head_cursor` is the highest currently available public sequence, or zero for
an empty stream. Process `entries` idempotently in sequence order, then persist
`next_cursor` only after the entire batch is committed locally. Entries contain
entity ID/kind, slug, change type, changed scopes, and projection version. The
feed is gap-free; do not substitute “latest updated_at” polling.

The endpoint returns `409 application/problem+json` with the current
`stream_id` and `head_cursor` when synchronization cannot safely continue:

- `code: change_stream_changed` means the database was rebuilt and now has a
  different stream identity;
- `code: change_cursor_ahead` means the consumer cursor is newer than the
  available public head, such as after restoring an older database snapshot.

For either code, discard the stored cursor, reset it to zero, adopt the returned
`stream_id`, and replay the feed idempotently. Do not continue from the old
cursor. A cursor exactly equal to `head_cursor` is valid and returns an empty
page.

Large JSON reads support `Accept-Encoding: gzip`; keep normal HTTP automatic
decompression enabled. Entity, credit, artwork-list, and change-feed responses
also expose a `Server-Timing` database/read-model phase (`entity`, `credits`,
`images`, or `changes`) so network time can be separated from origin work.

Recommended cache key:

```text
entity:{id}:projection:{projection_version}:locale:{locale-key}
```

Locale is part of the representation. Respect `Vary: Accept-Language`.

## Credentials and authentication

There are two unrelated credential concepts.

### Heya user/API-key authentication

The account endpoints support browser sessions and Heya bearer API keys. In the
current implementation, normal metadata reads and workflow operations are not
gated by a Heya bearer key. `Authorization: Bearer ...` is currently consumed
by `GET /api/v2/auth/me`; it is not an upstream provider credential.

Do not assume this will always remain anonymous. Generate from OpenAPI security
requirements if service authentication is added later.

### Request-scoped upstream provider credentials

Users may supply their own provider keys. Forward them only in the documented
headers:

| Header | Provider credential |
| --- | --- |
| `X-Heya-TMDB-API-Key` | TMDB token/key |
| `X-Heya-OMDB-API-Key` | OMDb key |
| `X-Heya-TVDB-API-Key` | TVDB key |
| `X-Heya-Fanart-API-Key` | Fanart.tv personal key |
| `X-Heya-Apple-API-Key` | Apple developer token when direct Apple Music is configured; iTunes search itself needs none |
| `X-Heya-Discogs-API-Key` | Discogs token |
| `X-Heya-LastFM-API-Key` | Last.fm key |
| `X-Heya-Google-Books-API-Key` | Google Books key |
| `X-Heya-MAL-Client-ID` | MyAnimeList client ID |
| `X-Heya-AcoustID-API-Key` | AcoustID key for fingerprint matching |

Forward applicable keys on resolution, generic entity reads, and explicit
refreshes. Generic discovery currently accepts a request-scoped TMDB key for
movie discovery; other currently implemented discovery paths may not require a
user key. That provider choice is private to HeyaMetadata. Enrichment workers
receive credentials through short-lived opaque Redis references. Keys are never
written to Postgres or River job arguments.

The HeyaMedia implementation must preserve that property:

- never include keys in URLs, job payloads, structured logs, traces, error
  strings, or cache keys;
- redact all `X-Heya-*` headers in HTTP logging;
- keep credentials in request/user secret storage only;
- do not send a provider key in `Authorization`;
- forward only the keys relevant to the request.

## Complete public endpoint inventory

### Core identity and entity operations

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v2/search` | Fast local canonical search for query-only matching |
| POST | `/api/v2/discoveries` | Reconcile identifiers or discover from query/hints |
| GET | `/api/v2/discoveries/{id}` | Discovery status/result |
| POST | `/api/v2/resolutions` | Resolve a selected opaque candidate when required |
| GET | `/api/v2/jobs/{id}` | Durable River job status |
| GET | `/api/v2/entities/{id}` | Universal combined canonical detail |
| POST | `/api/v2/entities/{id}/refreshes` | Explicit interactive refresh |
| GET | `/api/v2/entities/{id}/relations` | Paginated canonical/provider relations |
| GET | `/api/v2/entities/{id}/images` | Language-aware artwork selection |
| GET | `/api/v2/entities/{id}/credits` | Cast/crew for movie, TV, and anime |
| GET | `/api/v2/entities/{id}/ratings` | Provider-native ratings |

### Domain and resource operations

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v2/tv/shows/{id}` | Conventional TV detail |
| POST | `/api/v2/tv/discoveries` | TV discovery convenience route |
| GET | `/api/v2/anime/{id}` | Anime detail |
| POST | `/api/v2/anime/discoveries` | Anime discovery convenience route |
| GET | `/api/v2/seasons/{id}` | Standalone season resource |
| GET | `/api/v2/episodes/{id}` | Standalone episode resource |
| GET | `/api/v2/releases/{id}` | Issued music release detail |
| GET | `/api/v2/recordings/{id}` | Recording detail |
| GET | `/api/v2/recordings/{id}/fingerprints` | Stored Chromaprint evidence |
| GET | `/api/v2/recordings/{id}/lyrics` | Plain/synchronized lyrics evidence |
| POST | `/api/v2/fingerprint-matches` | Match a client fingerprint |
| GET | `/api/v2/fingerprint-matches/{id}` | Fingerprint-match status/result |
| GET | `/api/v2/persons/{id}` | Canonical person and filmography |
| GET | `/api/v2/persons/{id}/credits` | Canonical-person reverse filmography |
| GET | `/api/v2/manga/{id}` | Manga series detail |
| POST | `/api/v2/manga/discoveries` | Manga discovery |
| GET | `/api/v2/manga/volumes/{id}` | Physical manga-volume detail |
| POST | `/api/v2/manga/volumes/discoveries` | Manga-volume discovery |
| GET | `/api/v2/comics/volumes/{id}` | Comic-volume detail |
| POST | `/api/v2/comics/discoveries` | Comic-volume discovery |
| GET | `/api/v2/images/{id}` | Original canonical image bytes/materialization |
| GET | `/api/v2/images/{id}/variants/{format}/{width}` | WebP/AVIF variant bytes/materialization |

### Library and synchronization operations

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v2/browse` | Paginated local library (`q`, `kind`, sort, offset, limit) |
| GET | `/api/v2/latest` | Recently updated canonical entities |
| GET | `/api/v2/stats` | Canonical library/provider/freshness counts |
| GET | `/api/v2/changes` | Gap-free public change feed |
| GET | `/api/v2/collections` | Known movie franchises |
| GET | `/api/v2/collections/{id}` | One movie franchise and members |

Browse sorting supports `updated`, `title`, `year`, and `popular`.

### System and account operations

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v2/health/live` | Process liveness |
| GET | `/api/v2/health/ready` | Dependency readiness |
| GET | `/api/v2/auth/challenge` | Optional registration/login proof-of-work challenge |
| POST | `/api/v2/auth/register` | Create local user and browser session |
| POST | `/api/v2/auth/login` | Create browser session |
| POST | `/api/v2/auth/logout` | End browser session |
| GET | `/api/v2/auth/me` | Resolve browser session or Heya bearer key |
| GET | `/api/v2/auth/api-keys` | List current user's key metadata |
| POST | `/api/v2/auth/api-keys` | Create a Heya API key; plaintext appears once |
| DELETE | `/api/v2/auth/api-keys/{id}` | Revoke a Heya API key |

The account/key-management routes are not required for the first server-to-
server metadata cutover unless HeyaMedia is also implementing user accounts.

## Go interfaces and generated client

The generated client package is:

```go
github.com/HeyaMedia/HeyaMetadata/sdk/go/heyametadata
```

For sibling-repository development, HeyaMedia can temporarily use:

```go
require github.com/HeyaMedia/HeyaMetadata v0.0.0

replace github.com/HeyaMedia/HeyaMetadata => ../HeyaMetadata
```

Use a tagged/pinned module version in deployment. Alternatively, generate a
client inside HeyaMedia from `api/openapi.yaml`; do not hand-maintain endpoint
request structs.

Construction:

```go
httpClient := &http.Client{Timeout: 10 * time.Second}

client, err := heyametadata.NewClientWithResponses(
    metadataURL,
    heyametadata.WithHTTPClient(httpClient),
    heyametadata.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
        req.Header.Set("User-Agent", "HeyaMedia/<version>")
        return nil
    }),
)
```

The generated `ClientWithResponsesInterface` is the low-level mockable
interface. Put the workflow behind a smaller HeyaMedia-owned interface so tests
do not depend on every generated operation:

```go
type MetadataResolver interface {
    Search(context.Context, SearchRequest) ([]CanonicalSummary, error)
    Discover(context.Context, DiscoveryRequest, ProviderCredentials) (Discovery, error)
    Resolve(context.Context, heyametadata.ResolutionInputBody, ProviderCredentials) (string, error)
    Entity(context.Context, string, Locale, ProviderCredentials) (CanonicalDocument, error)
}

type MetadataLibrary interface {
    Browse(context.Context, BrowseRequest) (Page[CanonicalSummary], error)
    Latest(context.Context, string, int, Locale) ([]CanonicalSummary, error)
    Changes(context.Context, string, int64, int) (ChangePage, error)
}
```

The generated workflow models (`Request`, `Hints`, `Candidate`,
`ResolutionInputBody`, `DiscoveryResource`, `JobResource`, image/credit/relation
resources, and so on) are typed. A resolution input contains only the opaque
`candidate_ref` returned by discovery. Several polymorphic canonical/search bodies
are intentionally generated as `interface{}` because the current OpenAPI
operation uses a union-like body. Use
`heyametadata.DecodeCanonicalDocument(response.Body)` (or
`DecodeCanonicalValue(response.JSON200)`) to obtain one of the exported
`Canonical*Document` wrappers keyed strictly by `kind`. Unknown kinds return
`ErrUnsupportedCanonicalKind`. Never import packages under HeyaMetadata's
`internal/`; Go prevents cross-module imports and those types are implementation
details.

Implementation model references, useful when defining the target adapter's
kind-specific DTOs, currently live at:

| Domain | Implementation reference |
| --- | --- |
| Movies | `internal/domains/movie/projection.go` |
| Artists | `internal/domains/artist/projection.go` |
| Release groups | `internal/domains/releasegroup/projection.go` |
| Issued releases/recordings | `internal/domains/release/model.go` |
| TV/anime | `internal/episodic/model.go` and `internal/episodic/resources.go` |
| Books/editions/authors | `internal/books/model.go` |
| Manga | `internal/manga/model.go` |
| People | `internal/people/service.go` |
| Musical works | `internal/musicalworks/model.go` |
| Artwork selection | `internal/images/selection.go` |

These paths explain the JSON; they are not a supported import surface.

Regenerate the HeyaMetadata contract after public API changes with:

```sh
make generate-api
make acceptance
make check-generated
```

### Typed generated-client asynchronous responses

Every legitimate asynchronous success is now declared as typed
`202 application/json` in OpenAPI. Generated wrappers expose `JSON202` for
discovery, dedicated discovery, resolution, refresh, fingerprint matching, and
image materialization. Inspect `StatusCode()` and consume `JSON200` or `JSON202`
as appropriate; no raw-body fallback is part of the supported contract.

## Error handling

API errors use `application/problem+json` with standard `type`, `title`,
`status`, `detail`, `instance`, and optional field errors. Synchronization
conflicts additionally carry `code`, `stream_id`, and `head_cursor`; handle
`change_stream_changed` and `change_cursor_ahead` using the reset-and-replay
procedure above.

Client policy:

- `400`/`422`: invalid request or schema validation failure; do not retry
  unchanged input indefinitely;
- `404`: unknown entity/resource or expired opaque candidate; fall through only where
  the identity state machine explicitly permits it;
- `409`: conflict such as account state; caller action required;
- `429`: honor `Retry-After` and apply jittered backoff;
- `500`/`502`/`503`: transient unless repeated; retry within the workflow's
  bounded deadline and retain durable discovery/job IDs;
- timeout after POST: retry the idempotent request or resume the known durable
  resource; never create a competing local entity.

Do not expose raw upstream errors or credential-bearing URLs to HeyaMedia
clients.

## Mapping the old HeyaMedia v1 surface

The old implementation under `../HeyaMedia/internal/api` mixes local reads,
upstream discovery, inline enrichment, and persistence. Replace it as follows:

| Old HeyaMedia behavior | v2 replacement |
| --- | --- |
| `GET /api/v1/search` upstream fan-out | identifier-bearing input goes directly to discovery; query-only input uses local `/api/v2/search`, then discovery on miss |
| `GET /api/v1/{kind}/{provider:id}` inline enrich | discovery with every known identifier → direct canonical read on `result.entity_id`, or opaque selection/resolution/job polling when ambiguous |
| `GET /api/v1/{kind}/{slug}` | stored Heya UUID or local search; slug is presentation, not identity |
| `GET /api/v1/recent` | `/api/v2/latest` |
| `GET /api/v1/browse` | `/api/v2/browse` |
| `GET /api/v1/stats` | `/api/v2/stats` |
| collection endpoints | v2 collection endpoints |
| arbitrary image proxy/old image registry | entity image selection + canonical image IDs/variants |
| person detail/filmography | `/api/v2/persons/{id}` and `/api/v2/persons/{id}/credits` |
| person batch hydration | no direct equivalent; use embedded person IDs and bounded reads |
| similar artist/track endpoints | combined canonical similarity/recommendation evidence where available |
| old Mongo change/refresh logic | v2 change cursor + stale-while-revalidate |

Do **not** blindly delete these old features with the metadata pipeline:

- `/api/v1/movie/{id}/segments`
- `/api/v1/tv/{id}/segments`
- `/api/v1/tv/{id}/segments/{season}/{episode}`
- hidden YouTube playback search, if the current frontend still uses it
- old `/browse/facets`, `/random`, or slug-compatibility routes until their
  callers are migrated or the routes are intentionally retired

Community skip segments are deliberately Heya-owned media-server behavior.
Move the TheIntroDB, SkipMeDB, and AniSkip clients plus their runtime-aware cache
into Heya before disabling the old service; HeyaMetadata will not add segment
mutation/read endpoints. Hidden YouTube playback search and remaining legacy
browse helpers likewise require an explicit Heya-side migration or retirement.

Audiobook-specific provider corroboration is deferred from the first V2
cutover. Publication discovery accepts general title, author, date, ISBN, and
format hints, but has no dedicated Audible canonicalization and makes no audiobook-specific
confidence promise. Heya must preserve ambiguity for audiobook candidates and
request user selection rather than auto-selecting on a title/author resemblance.

## Replacement order in HeyaMedia

1. Add a configurable HeyaMetadata HTTP client and health/readiness check.
2. Implement the resolver state machine behind a small HeyaMedia interface.
3. Add canonical raw-envelope and kind-specific DTO decoding. Do not reuse the
   old flattened `TitleDoc` as the new storage model.
4. Migrate scanner/import matching. Submit every exact identifier directly to
   discovery and let HeyaMetadata reconcile it before fuzzy name/title search.
   Query-only matching uses local search, then discovery on a miss. Resolution
   is used only when discovery returns opaque candidates requiring selection.
5. Persist Heya UUIDs on media/library records and backfill existing items.
6. Migrate detail reads, artwork, credits/people, discography/releases/tracks,
   TV seasons/episodes, books, and recommendations.
7. Replace old recent/browse/stats/collection reads or proxy the v2 equivalents.
8. Consume `/api/v2/changes` for cache/index invalidation, persisting both its
   stream identity and cursor and resetting to zero on either typed conflict.
9. Remove HeyaMedia provider clients, enrichment workers, metadata Mongo
   collections, Meilisearch metadata indexing, and old image metadata only
   after no migrated call site references them.
10. Retain the explicitly out-of-scope media-server features listed above.
11. Run unit, integration, failure, restart/resume, and live end-to-end tests.
12. Cut over without a silent fallback to the old provider pipeline; failures
    must be visible rather than creating divergent metadata.

If existing external clients still require `/api/v1`, implement a temporary
thin compatibility adapter over v2. It must not perform provider work or store
its own canonical document. Prefer moving clients to v2 directly.

## End-to-end acceptance plan

Run HeyaMetadata on localhost:3030 and HeyaMedia on a non-conflicting port.
Use no real provider secrets in fixtures, snapshots, or logs.

### Contract and health

- readiness is 200 and lists healthy dependencies;
- Scalar and OpenAPI are reachable;
- the HeyaMedia client can decode `application/problem+json`;
- generated-client and server contract drift checks pass.

### Search fast path

- ingest/resolve one known entity, then search it;
- assert search returns its `id` without creating a discovery/job;
- fetch by UUID and verify kind, display, projection version, freshness, and
  provenance;
- query a canonical name/alias and verify the local fast path remains
  side-effect free.

### Movie canary

- discover The Matrix in an empty test library with TMDB and/or IMDb evidence;
- accept either immediate 200 or 202 + job, then read the canonical movie;
- verify credits, required Heya person IDs, provider-native ratings,
  collection/recommendation links, and English artwork selection;
- request a WebP image variant and handle both initial 202 and final bytes.

### TV and anime separation

- discover a conventional show such as `Game of Thrones` with year/network
  hints and assert kind `tv_show`;
- discover an AniDB title through kind `anime` and assert it cannot collapse
  into the TV entity kind;
- discover `Attack on Titan` year 2013 with AniDB `9541`, IMDb `tt2560140`,
  TMDB `1429`, and TVDB `267440`; assert all evidence converges on the 2013
  Heya UUID and no selection is required;
- combine the later-season AniDB `10944` with those broad series IDs and assert
  it resolves to the same TMDB-rooted Heya anime while `10944` remains scoped
  to the canonical season-two resource rather than stealing broad claims;
- read a season and episode through their standalone UUIDs;
- verify credits/ratings and language-aware posters.

### Music freshness and namesakes

- discover `Atarashii Gakko!`, resolve it, and read `discography` relations;
- assert the reconciled catalog contains `Oi AG!`;
- discover `Ado`, resolve it, and assert the catalog contains
  `Love me forever!` under its localized or alternate title;
- discover Japanese artist `ano` with JP/alias/release hints and assert the
  selected evidence does not merge German or other namesake identities;
- resolve an Apple-only and a Deezer-only artist to canonical Heya UUIDs, then
  verify their storefront releases appear without a MusicBrainz claim;
- submit Yoshiko's Apple artist `591024034` plus release `1630125755` and assert
  `Freaks Out` is linked to the canonical Yoshiko discography;
- verify `Blank & Jones`, `Balloon` (the Monstersound/Pussylovers artist), GPF,
  Valknee, PaleNeØ, and ハク/Haku remain individually selectable when matching
  hints distinguish them;
- traverse artist → release group → issued release → track → recording;
- assert resolved tracks expose `recording_entity_id`;
- verify duplicate kana/romaji release presentations do not create duplicate
  canonical release groups;
- read fingerprints and lyrics for a recording when evidence exists.

### Publications and people

- discover and resolve a book work with author/ISBN hints;
- verify work, edition, and author IDs remain distinct;
- discover manga and a physical manga volume through their separate kinds;
- traverse a movie credit to a canonical person and verify reverse filmography.

### Asynchrony, idempotency, and recovery

- force `Prefer: respond-async`, restart the HeyaMedia workflow, and resume from
  the persisted discovery/job ID;
- submit identical discovery and resolution requests concurrently and assert
  one canonical entity results;
- retry after a simulated response timeout;
- assert stale reads return content and do not trigger new discovery;
- verify terminal failures do not persist a successful local mapping.

### Change feed and cache

- save stream identity and cursor 0, ingest/refresh an entity, consume its
  change entry, and commit `next_cursor` only after local work succeeds;
- replay the same page and assert idempotency;
- replace the database or restore an older snapshot and assert the typed 409
  resets consumption to cursor zero instead of silently stalling;
- verify locale-specific cached displays do not leak between languages;
- verify a higher `projection_version` replaces an older cached document.

### Credential hygiene

- inject sentinel provider keys through the adapter;
- assert they reach only the intended `X-Heya-*` headers;
- inspect captured logs, job rows, database rows, cache keys, errors, and traces
  and assert the sentinel values never appear;
- assert Heya bearer auth and provider credentials are never interchanged.

### Cutover proof

- disable/remove old upstream provider clients and metadata stores in the test
  wiring;
- run scanner → match → discovery → direct canonical fetch or, only for an
  ambiguous result, opaque resolution/job polling → artwork and
  relationship traversal end to end;
- stop HeyaMetadata and assert HeyaMedia reports dependency failure instead of
  silently writing old-format metadata;
- restart HeyaMetadata and assert durable workflows resume without duplicates.

## Definition of done

The replacement is complete when:

- every HeyaMedia metadata consumer uses a Heya UUID;
- identifier-bearing requests go directly through discovery, while query-only
  misses use local search then discovery rather than inline provider fan-out;
- discovery `result.entity_id` proceeds directly to canonical read;
- only a `needs_selection` result uses opaque `candidate_ref` resolution and
  optional job polling before canonical read;
- ambiguous candidates are not silently auto-selected;
- combined data, localization, provenance, artwork, relations, credits,
  ratings, releases, recordings, books, seasons, and episodes are reachable;
- stale reads and the change cursor drive refresh/cache behavior;
- provider credentials are request-scoped and absent from durable/logged state;
- no active call site imports the old enrich/provider/search/image-metadata
  implementation;
- explicitly out-of-scope media-server features still work or were consciously
  retired;
- the end-to-end acceptance plan passes with old metadata infrastructure
  disabled.
