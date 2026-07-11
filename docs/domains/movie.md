# Movie vertical slice

Status: TMDB vertical slice implemented for schema version 1; additional
providers expand the same collector, mixer, normalizer, and combiner pipeline.

This document applies the platform decisions in `HeyaMetadataV2.md` to the
first complete entity slice. It deliberately does not freeze SQL column names
or every public JSON property. The identity boundaries, normalized facts,
merge behavior, provenance scopes, and required API behavior are the contract.

## Goals

The movie slice proves the complete v2 path:

```text
provider request
  -> immutable observation and content-addressed blob
  -> versioned normalized movie record
  -> external-ID resolution
  -> canonical movie merge
  -> detail, summary, and search projections
  -> transactional outbox
  -> sequenced change entry
  -> Postgres read and Redis cache
```

TMDB is the first adapter because it supplies the broadest single movie record.
It is a bootstrap source, not the owner of movie identity. OMDb, TVDB, and
Fanart.tv follow through the same observation and normalization boundary.

The acceptance surface is the executable catalog in `coverage/movie.json`.

## Legacy coverage inventory

The previous HeyaMedia movie path was inspected only for useful facts and known
provider behavior. It supplied:

- TMDB detail plus credits, external IDs, keywords, regional release dates,
  videos, recommendations, artwork, alternative titles, translations, and
  collection detail.
- OMDb plot fallback plus IMDb, Rotten Tomatoes, and Metacritic ratings.
- TVDB aliases, genres, tags, companies, people, remote IDs, and artwork.
- Fanart.tv posters, backdrops, logos, banners, clear-art, thumbnails, and disc
  art.
- Per-provider success/failure state and lazy image materialization.

V2 retains those facts with stronger identity and provenance. It does not
retain the old document envelope, TMDB/IMDb-derived canonical IDs, embedded raw
responses, name-based credit deduplication, or upstream-URL image identity.

## Canonical boundaries

### Movie

A movie is a released, planned, or cancelled audiovisual work intended as one
movie-level work. The canonical movie receives an opaque immutable ID. Title,
year, slug, and provider IDs may all change without changing that ID.

Different cuts, restorations, regional releases, physical editions, and
streaming encodes are not separate canonical movies by default. They are
release events or future edition/presentation records when a product need
requires that distinction. A provider object is not automatically canonical.

The following are separate canonical entities when resolved:

- people credited in cast or crew;
- production companies;
- movie collections/franchises; and
- other movies related as recommendations or collection members.

Genres, keywords, ratings, videos, certifications, and release events are
typed facts or provider-scoped records, not canonical entities in version 1.

### Credits

A credit relates a movie to a person and carries role-specific data. Cast
credits include character text and order. Crew credits include department,
job, and order when known. Two provider credits may be joined only through
accepted person external-ID claims or an explicit identity decision. Names are
display evidence, never sufficient identity keys.

When the person is unresolved, the normalized credit remains attached to its
source record and can appear as an unresolved credit projection. Later person
resolution upgrades the relation without changing the movie ID.

### Collections and recommendations

A TMDB collection is an identity claim for a canonical collection, not an
embedded movie attribute. Its ordered member list becomes canonical relations
as members resolve. Unresolved members retain provider target IDs and display
summaries in normalized data.

Recommendations are directed, source-scoped relations. They do not imply
identity or canonical similarity. A recommendation may point to an unresolved
provider target until that target is enriched.

## External identifier claims

Initial namespaces are:

| Provider | Namespace | Normalization |
| --- | --- | --- |
| TMDB | `movie` | positive base-10 integer rendered without leading zeroes |
| IMDb | `title` | lowercase `tt` followed by digits |
| TVDB | `movie` | positive base-10 integer rendered without leading zeroes |
| Wikidata | `item` | uppercase `Q` followed by digits |
| EIDR | `title` | canonical case and punctuation defined by an EIDR normalizer |

Social and Wikipedia identifiers may be retained as typed links or claims, but
they do not participate in automatic movie identity without a documented
namespace rule.

An active `(provider, namespace, normalized_value)` claim maps to at most one
active canonical entity within the movie boundary. Conflicting claims create a
reconciliation record. They never silently merge two movies.

### Initial resolution policy

1. Resolve every normalized external ID from the record.
2. If no active claims resolve, create a movie and attach proposed claims in the
   same transaction after conflict checks.
3. If all resolved claims identify one movie, attach the observation there.
4. If resolved claims identify multiple movies, stop the merge and create an
   external-ID conflict.
5. Title/year similarity may rank candidates for review but cannot join them
   automatically in version 1.

## Normalized movie record v1

Each provider adapter emits a typed logical record. The concrete Go type may
use nested structs and JSONB serialization, but it contains these scopes:

```text
NormalizedMovieRecordV1
  provider_record
    provider, namespace, value
    observation_id, observed_at
    normalizer_version, schema_version
    warnings, partial_failure
  identity_candidates[]
    provider, namespace, normalized_value, confidence, evidence
  titles[]
    value, language, country, type
  descriptions[]
    value, language, country, type
  classification
    provider_media_type, genres[], tags[], keywords[]
    original_language, spoken_languages[], countries[]
    animation_evidence
  lifecycle
    raw_status, normalized_status
    release_events[] {country, type, date, certification, note}
  measurements
    runtime_minutes
    budget {amount, currency, currency_basis}
    revenue {amount, currency, currency_basis}
    popularity {value, scale_or_meaning}
  ratings[]
    system, value, scale_min, scale_max, votes, raw_value
  links[]
    kind, url_or_provider_key, language, country
  videos[]
    host, key, type, name, language, country, official, published_at
  companies[]
    provider_identity, name, role, country, logo_candidate
  credits[]
    provider_person_identity, display_name, credit_type
    character, department, job, order, profile_candidate
  images[]
    provider_image_identity, source_url, class
    width, height, language, country, provider_score, likes
  collection
    provider_identity, name, overview, images[], members[]
  recommendations[]
    provider_target_identity, title, year, image, provider_score
```

Missing and explicit zero values remain distinguishable. Adapters preserve raw
rating scales instead of coercing Rotten Tomatoes or Metacritic into a 0–10
number. TMDB financial values record the adapter's currency interpretation;
unknown currency is not silently presented as certain.

Adapter warnings are data-quality signals, not ordinary provider transport
errors. A record with useful partial data may normalize successfully with
warnings. The exact raw bytes always remain reachable through the observation.

## Provider plans

### TMDB

Fetch movie detail with credits, external IDs, keywords, release dates, videos,
recommendations, images, alternative titles, and translations. Fetch collection
detail separately when the movie declares a collection. Each HTTP response is
its own observation and blob; a normalized record can cite both observations.

TMDB failure prevents a TMDB-seeded first import, but it does not define the
long-term existence of a canonical movie. Once a movie exists, TMDB failure is
one provider state and does not erase other canonical facts.

TMDB raw response blobs use the `data/ephemeral/48h/` RustFS lifecycle tier.
Normalized records and observation metadata remain durable after raw bytes
expire.

### OMDb

Fetch only when an accepted IMDb title claim exists. Preserve its plot and
rating evidence. OMDb's IMDb, Rotten Tomatoes, and Metacritic values are
separate rating systems with separate scales. An OMDb `Response: False` result
is an observed provider miss, not malformed successful metadata.

### TVDB

Fetch only when an accepted TVDB movie claim exists. Normalize aliases, genres,
tags, companies, remote IDs, characters/people, content ratings, and artwork.
TVDB remote IDs are claims and must pass namespace-specific normalization.

### Fanart.tv

Fetch using an accepted TMDB movie claim. Normalize artwork candidates by
class. Failure affects artwork coverage only. Fanart likes are provider ranking
signals, not global image quality scores.

## Merge policy v1

Merge rules are deterministic and versioned as `movie-merge/v1`.

### Scalar selection

- Preferred display title: configured-locale title, then English title, then
  original title, then another non-empty official title. Provider preference
  breaks ties only after locale and title type.
- Original title and language: highest-confidence original-title claim from the
  provider precedence configured for movies.
- Preferred overview: configured locale, then English, then original language;
  TMDB precedes OMDb at equal locale/type in version 1.
- Runtime: retain all provider claims; select the highest-priority plausible
  value and surface disagreement in provenance diagnostics.
- Lifecycle status: normalize provider values, then select the most recent
  trusted observation. A terminal status is not downgraded by an older record.
- Budget and revenue: select only values with known interpretation. Zero from a
  provider that uses zero for unknown does not beat an actual value.

Provider precedence is configuration owned by the merge version, not scattered
through adapters.

### Set and ordered values

- Titles, aliases, genres, tags, keywords, release events, ratings, videos, and
  images are unioned using type-specific keys and retain all contributing
  records.
- Localized text deduplicates by normalized value, locale, country, and type;
  provenance may list multiple contributing records.
- Ratings deduplicate by rating system and observation period, never merely by
  numeric value.
- Cast ordering follows the selected primary credit source. Secondary credits
  enrich only when person identity and role identity match.
- Crew deduplication requires person identity plus normalized department/job.
- Images deduplicate provider candidates by provider image identity or stable
  source identity. Content checksum deduplication happens later during
  materialization and does not collapse distinct candidate provenance.

### Overrides and rebuilds

Accepted user overrides form a separate input layer and win only for their
declared scope. A merge rebuild consumes retained normalized records plus active
overrides. It does not fetch providers. Projection writes compare canonical and
projection versions so an older job cannot replace newer output.

## Provenance scopes

Version 1 records winners or contributors for:

- display title, original title, and each localized title;
- preferred overview and each localized overview;
- status, primary release date, and each regional release event;
- genres, tags, keywords, languages, and countries;
- runtime, financials, popularity, and each rating system;
- every external-ID claim;
- each company, credit, collection-member, and recommendation relation;
- each video and image candidate; and
- the selected display image.

The storage representation may group provenance by logical path. The public
detail document exposes concise provenance references; the operator inspector
can expand them to normalized records and observations.

## Serving projections

All documents contain `schema_version`, opaque `id`, `kind`, current `slug`,
external IDs, freshness, links, and a projection version.

### Summary

Used by search, browse, collection members, recommendations, and cards:

```text
id, kind, slug
display {title, original_title?, year?, image_id?}
attributes {status?, genres[], countries[], original_language?}
external_ids[]
freshness {state, updated_at}
```

### Detail

Adds localized titles and overviews, tagline, release events,
certifications, runtime, financials, ratings, studios, links, videos, selected
credits, collection, recommendations, and image candidates. Large credit lists
are cursor-paginated; `include=credits` may embed a bounded first page.

### Snapshot

Provides one projection-version-consistent movie bundle for a Heya media
server. It may include larger bounded relationship sets, but it still links to
paginated resources when limits are exceeded.

Suggested resources:

```text
GET  /api/v2/entities/{movie_id}
GET  /api/v2/entities/{movie_id}/snapshot
GET  /api/v2/entities/{movie_id}/credits
GET  /api/v2/entities/{movie_id}/images
GET  /api/v2/entities/{movie_id}/recommendations
POST /api/v2/resolutions
POST /api/v2/entities/{movie_id}/refreshes
GET  /api/v2/jobs/{job_id}
```

Canonical entity routes use only opaque IDs. Resolution accepts typed external
IDs and returns a completed entity or a typed job resource. Slug routes include
the kind and resolve through slug history.

## Search projection

`search_entities` stores the compact movie summary, release year, status,
countries, languages, genres, tags, studios, popularity signals, and freshness.
`search_names` stores canonical, original, localized, and alternative titles
with locale, type, source quality, and normalized search form.

Query order is:

1. exact external ID;
2. exact normalized name;
3. normalized prefix;
4. trigram candidates; and
5. optional full-text behavior for longer descriptive queries later.

All result rows return the opaque movie ID. One- and two-character names must
work through exact/prefix indexes without depending on trigrams.

## Jobs and transaction boundaries

Initial job arguments are versioned and use deterministic unique keys:

```text
fetch_movie_provider(movie-or-external-id, provider, fetch_policy_version)
normalize_movie_observation(observation_id, normalizer_version)
merge_movie(movie_id, merge_version)
rebuild_movie_projections(movie_id, projection_schema_version)
materialize_image(image_id, transform_version)
```

An orchestration job may enqueue these stages, but each stage remains safely
retryable. Provider observations are inserted even when bytes deduplicate.
Normalized-record insertion is idempotent on observation and normalizer
version. Merge output is idempotent on its complete input version set.

The canonical update, API-document update or rebuild marker, search update or
rebuild marker, and unsequenced outbox row commit together. The sequencer later
assigns a public change cursor.

## Freshness and failure behavior

Freshness is provider-aware. A movie projection can be usable while Fanart.tv
or OMDb is failing. Each expected provider records last attempt, last success,
last useful observation, failure class, next eligible refresh, and current job.

- Fresh movie: serve cache or Postgres projection.
- Stale movie: serve immediately and enqueue a unique interactive refresh.
- Missing external ID: enqueue interactive resolution/fetch and wait within the
  endpoint budget; otherwise return `202` with the durable job.
- Primary bootstrap fetch not found: complete the job as a typed provider miss,
  suitable for a short negative cache.
- Rate limit or transport failure: preserve state and retry with provider policy.
- Conflicting external IDs: stop automatic identity work and expose an operator
  reconciliation item.

## Image behavior and retention

Normalization creates an opaque image candidate ID before downloading bytes.
The candidate stores provider, class, source URL, language, country, dimensions,
scores, observation, license/retention class, and materialization state.

On demand, a worker validates the source, writes or reuses the content-addressed
blob, and creates only declared serving variants. Movie artwork may retain an
original when its license and retention class allow it. Cast/crew profile
originals are not retained by default. Public routes accept image ID plus a
declared transform, never an arbitrary URL.

## First implementation milestone

The movie slice is ready for the next provider only when a TMDB reference movie
can pass through the real local platform and prove:

1. Postgres migration and River job insertion;
2. immutable compressed raw response in S3-compatible storage plus observation;
3. normalized movie record v1;
4. opaque movie identity and accepted TMDB/IMDb claims;
5. deterministic merge and field provenance;
6. detail and search projections;
7. Redis read cache and invalidation;
8. transactional outbox and gap-free sequenced change entry;
9. stale-while-refresh and typed `202` behavior; and
10. catalog-backed integration assertions for the implemented TMDB entries.

Then add OMDb, TVDB, and Fanart.tv one at a time. Each adapter expands passing
catalog entries without bypassing the pipeline.

## Deferred decisions

These do not block the first TMDB milestone but must be resolved before the
affected public behavior is declared stable:

- exact currency semantics for every financial provider;
- whether movie presentations/editions become canonical entities;
- collection merge rules beyond one provider identity;
- public provenance verbosity versus inspector-only detail;
- maximum embedded credit/image/recommendation counts; and
- provider-specific raw-blob and artwork-original retention periods.
