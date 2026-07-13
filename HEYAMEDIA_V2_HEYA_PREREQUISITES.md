# HeyaMetadata V2 prerequisites for the Heya migration

Status: blocking handoff contract
Audience: HeyaMetadata implementers and reviewers
Consumer: Heya (the media-server repository at `../Heya`)
Related document: `HEYAMEDIA_V2_MIGRATION.md`

## 1. Purpose

This document describes what must exist, how it must behave, and what evidence
must be supplied before the Heya repository can safely replace its current
`heya.media` metadata interface with HeyaMetadata V2.

The target is not merely that Heya can compile against a new base URL. The
target is that the old metadata service can be disabled while scanner matching,
enrichment, local read models, artwork, people, music, books, TV/anime, and
refresh behavior continue to work without silent metadata loss or identity
forks.

This document is intentionally consumer-shaped. It does not require V2 to copy
the old flattened Heya response. It defines the semantic information and API
behavior Heya needs; Heya will adapt the canonical V2 documents into its local
library read models.

Normative terms are used as follows:

- **MUST**: blocks the migration or final cutover.
- **SHOULD**: is strongly recommended; omitting it requires an explicit,
  documented product decision and an alternative.
- **MAY**: can be deferred without blocking the migration.

## 2. Readiness decision

As of this document's creation, HeyaMetadata V2 is not ready for a
no-regression Heya cutover.

The primary missing requirements are:

1. complete TV/anime, season, and episode projections;
2. canonical artist top tracks;
3. an explicit replacement for the old service's community segment endpoints;
4. a generated API contract that accurately models asynchronous `202`
   responses;
5. book-series support, or an explicit product decision to drop it;
6. consumer-shaped acceptance coverage proving the old service can be disabled.

Movies, people, canonical discovery/resolution, change feed concepts, artwork
materialization, and most music and publication identity are suitable
foundations. They still need end-to-end evidence, but they do not presently
appear to require a redesign.

## 3. Scope and ownership

### 3.1 HeyaMetadata owns

HeyaMetadata MUST own:

- upstream metadata provider access;
- provider-response normalization;
- canonical metadata identity and reconciliation;
- discovery candidates and their identity evidence;
- canonical entity projections and provenance;
- metadata freshness and refresh scheduling;
- canonical artwork identity and materialization;
- canonical relationships, credits, ratings, releases, recordings, works,
  seasons, and episodes;
- a gap-free public change feed;
- request-scoped forwarding of provider credentials where supported.

### 3.2 Heya owns

Heya will own:

- library files and file-to-entity matching state;
- local playback and user state;
- local relational read models used by its UI and protocols;
- scanner decisions, including manual disambiguation;
- cached copies of canonical projections governed by `projection_version` and
  the change cursor;
- media-server-only features such as community skip segments, unless the team
  explicitly assigns those features to HeyaMetadata.

### 3.3 Heya will not require

Heya will not require V2 to reproduce the old monolithic `MediaDetail` JSON
shape. It will consume kind-specific canonical documents and relationship
resources.

Heya will also not treat provider IDs, slugs, or display names as canonical
identity. It needs stable HeyaMetadata UUIDs.

## 4. Readiness gates

All start gates MUST be complete before substantial Heya adapter and persistence
work begins. All cutover gates MUST be complete before the old metadata service
is disabled.

| Gate | Required point | Evidence |
| --- | --- | --- |
| Public contract is stable and generated | Start | committed OpenAPI, regenerated client, drift check |
| Dynamic `200`/`202` responses are typed | Start | contract tests for every async operation |
| TV/anime season and episode shapes are complete | Start | DTOs, provider canaries, projection tests |
| Episode numbering semantics are documented | Start | fixtures for regular episodes, specials, and anime absolute numbering |
| Artist top tracks contract exists | Start | endpoint/projection, Last.fm canary, coverage entry |
| Book-series decision is recorded | Start | implemented contract or accepted-removal decision |
| Existing movie/person/music/book capabilities remain green | Start | targeted domain and coverage suites |
| Segment ownership is decided and implemented | Cutover | segment workers pass with old service unavailable |
| Change-feed consumer behavior is proven | Cutover | replay/idempotency/restart tests |
| Full Heya integration run passes | Cutover | scanner-to-artwork run with old metadata infrastructure disabled |

## 5. Cross-cutting API contract

Much of this is already described in `HEYAMEDIA_V2_MIGRATION.md`. The following
items are explicit prerequisites because the Heya adapter and durable workflows
depend on them.

### 5.1 Canonical identity

Every root entity returned to Heya MUST have:

```json
{
  "schema_version": 1,
  "projection_version": 42,
  "id": "4f93cc0a-7b7b-49ea-a8f3-5af2f11db2cf",
  "kind": "movie",
  "slug": "the-matrix",
  "external_ids": [
    {"provider": "tmdb", "namespace": "movie", "value": "603"}
  ],
  "freshness": {
    "state": "fresh",
    "updated_at": "2026-07-13T10:00:00Z",
    "fresh_until": "2026-07-20T10:00:00Z"
  }
}
```

Requirements:

- `id` MUST be stable across refreshes and projection changes.
- `kind` MUST be stable and drawn from the public kind vocabulary.
- `slug` MUST be presentation only and MUST NOT be required for identity.
- `projection_version` MUST increase whenever the public canonical projection
  changes.
- External IDs MUST preserve provider and namespace; consumers must never have
  to infer a namespace from the provider name.
- Entity merges MUST expose redirects or another deterministic way to recover
  the surviving UUID.
- Entity deletion/tombstone behavior MUST be represented in the change feed.

The public kind vocabulary consumed by Heya MUST include at least:

```text
movie
tv_show
anime
artist
release_group
release
recording
musical_work
book_work
book_edition
author
person
```

Season and episode resources may use their existing dedicated resource types;
their resource UUIDs MUST be stable and bookmarkable.

### 5.2 Search, discovery, and resolution

The intended workflow MUST remain:

```text
local search
  -> upstream discovery only on miss
  -> explicit candidate selection
  -> resolution of only the selected candidate
  -> job polling when asynchronous
  -> canonical entity read
```

Requirements:

- Search MUST be side-effect free.
- Discovery MUST NOT create canonical identity.
- A discovery candidate MUST contain a complete, opaque `resolution` payload;
  Heya MUST NOT reconstruct provider namespaces or resolution input.
- Candidates SHOULD include evidence suitable for safe selection: title/name,
  alternate names, year/date, provider IDs, image, type/kind, and the structured
  hints that matched.
- Existing matches MUST return `existing_entity_id` when known.
- Ambiguous candidates MUST remain distinguishable. V2 MUST NOT silently merge
  them merely because names are similar.
- Resolution requests MUST be idempotent under retries and concurrency.
- Identical concurrent resolutions MUST result in one canonical entity.
- Durable discovery and job resources MUST remain readable long enough for a
  Heya scanner workflow to resume after a process restart.
- `Retry-After` SHOULD be supplied on queued/working responses.

### 5.3 Asynchronous success responses

The OpenAPI contract MUST explicitly type every success status that can occur.
At minimum this applies to:

- discovery creation and polling;
- resolution creation;
- refresh requests;
- fingerprint matching;
- image materialization.

An operation that can return both immediate and queued results MUST declare both
`200 application/json` and `202 application/json` response bodies. The generated
Go client MUST expose both as typed fields. Heya must not have to unmarshal an
otherwise undocumented `response.Body` to recover a valid success resource.

Example behavior:

```http
HTTP/1.1 202 Accepted
Content-Type: application/json
Retry-After: 1

{
  "state": "queued",
  "job": {
    "id": "01J...",
    "state": "queued"
  }
}
```

Required contract test:

1. force each operation down its asynchronous path;
2. call it through the generated client;
3. assert the typed `202` field is populated;
4. poll using the advertised resource/job ID;
5. assert terminal success returns the canonical entity or materialized image.

### 5.4 Error contract

All non-success API errors MUST use `application/problem+json` and include:

- HTTP status;
- stable problem `type`;
- human-readable `title` and `detail`;
- `instance` where useful;
- field errors for invalid requests;
- a distinction between retryable upstream failure, validation failure,
  ambiguity, not-found, unauthorized, and terminal job failure.

Raw provider responses, provider credentials, and credential-bearing URLs MUST
never appear in public errors, logs, traces, job rows, cache keys, or durable
request bodies.

### 5.5 Pagination and bounded embedded previews

Every unbounded collection MUST have a documented pagination contract and stable
ordering. This includes:

- credits;
- relations/discography;
- ratings if they can grow without bound;
- images;
- collection members;
- filmography;
- top tracks if implemented as a dedicated endpoint;
- the change feed.

Embedded data may be a bounded preview only if the dedicated complete resource
is documented and available. Responses MUST make it possible to distinguish a
complete list from a truncated preview.

Cursor behavior MUST be deterministic under repeated requests. Page sizes and
maximums MUST be documented in OpenAPI.

### 5.6 Locale behavior

Canonical detail, search, browse, and artwork selection MUST have stable locale
semantics:

1. explicit `language` query;
2. ordered `fallback_languages`;
3. `Accept-Language`;
4. neutral/provider fallback.

`country` MUST be available where region affects titles, releases,
certifications, or artwork.

Localized values SHOULD preserve language, country, and value type rather than
being flattened to a map whose conflict behavior is undefined.

The test suite MUST prove that cached Danish, English, and Japanese displays do
not leak into one another.

### 5.7 Images

All durable public metadata MUST use opaque image IDs. Provider URLs may exist
internally as source evidence but MUST NOT be the durable public identifier.

For every image-bearing resource, Heya needs:

- image ID;
- class, such as poster, backdrop, profile, still, logo, banner, cover, disc, or
  thumbnail;
- provider/provenance;
- language and country when applicable;
- width and height when known;
- deterministic selection behavior.

The image byte/variant endpoint MUST:

- support at least the formats and sizes Heya will configure;
- return bytes immediately when materialized;
- return a typed `202` job resource when materialization is required;
- be idempotent for identical variant requests;
- return cache validators suitable for local/proxy caching;
- avoid exposing upstream signed URLs.

Season posters and episode stills are specifically required; show-level posters
alone are not sufficient.

### 5.8 Change feed

`GET /api/v2/changes` MUST be gap-free and replayable.

Each entry MUST contain enough information to:

- identify the canonical entity or resource;
- identify kind/scope;
- compare projection versions;
- recognize redirects, deletion/tombstones, and structural changes;
- invalidate a cached projection and derived local read model.

When a season, episode, track placement, issued release, credit, image selection,
or relation changes, the feed MUST identify a parent/root entity Heya can
re-fetch. It is acceptable to emit the show/artist/release-group root as long as
that behavior is documented and complete.

Cursor requirements:

- cursor zero can bootstrap a fresh consumer according to a documented policy;
- `next_cursor` advances monotonically;
- replaying a page is safe;
- Heya can commit the cursor only after its local transaction succeeds;
- cursor expiration/retention behavior is documented;
- there is a supported full-resync path if a cursor becomes invalid.

### 5.9 Readiness and version compatibility

`/health/ready` MUST fail when a dependency required for correct reads/writes is
unavailable and SHOULD report dependency state without secrets.

HeyaMetadata SHOULD expose a build/API version. The OpenAPI schema version and
the canonical document `schema_version` MUST have documented compatibility
rules. Breaking response changes require a schema/API version change, not an
in-place semantic mutation.

## 6. TV and anime: blocking projection requirements

This is the largest current functional gap.

The current public `episodic.Season` only exposes ID, provider ID, number, name,
episode order, and dates. The current `episodic.Episode` only exposes IDs,
titles, number schemes, air date, runtime, and one summary. Heya currently
persists and uses more information than those types can carry.

The work MUST update the complete path:

```text
provider collection
  -> provider normalizer
  -> normalized record
  -> merge rules
  -> canonical persistence
  -> public show/season/episode projections
  -> image candidates/materialization
  -> OpenAPI/generated client
  -> coverage catalog and canaries
```

Adding JSON fields only at the public handler is not sufficient.

### 6.1 Show requirements

A `tv_show` or `anime` canonical document MUST expose:

- canonical UUID, kind, external IDs, projection version and freshness;
- localized primary, original, alternate, and regional titles;
- localized overviews;
- first and last air dates;
- lifecycle/status;
- original language and origin countries;
- genres and tags/keywords;
- default episode runtime and total episode count;
- number of seasons where known;
- source material for anime where known;
- structured networks;
- structured studios/production companies;
- creators/created-by credits;
- full cast and crew through the dedicated credits projection;
- provider-native ratings with value, scale, vote count and provider;
- show artwork, including posters, backdrops, logos and banners when available;
- homepage/external links;
- videos/trailers;
- content certifications/ratings by country;
- recommendations/similar shows where providers supply them;
- child season and episode UUIDs.

Structured networks/studios MUST preserve identity where possible:

```json
{
  "entity_id": "optional-canonical-company-uuid",
  "name": "HBO",
  "type": "network",
  "country": "US",
  "external_ids": [
    {"provider": "tmdb", "namespace": "network", "value": "49"}
  ],
  "logo_image_id": "optional-opaque-image-uuid"
}
```

A canonical company entity is not required to unblock Heya, but provider
identity and logo image identity MUST not be discarded if collected.

### 6.2 Season requirements

Both embedded season entries and `GET /api/v2/seasons/{id}` MUST expose:

- stable season UUID;
- parent show UUID and show kind;
- season number;
- localized name/title;
- localized overview;
- premiere/air date and end date;
- status where known;
- total and aired episode counts where known;
- typed provider external IDs;
- poster and other season image IDs;
- ordered child episode UUIDs.

Example target shape. Exact field grouping may follow established V2
conventions, but equivalent semantics are required:

```json
{
  "id": "dfb7fbd0-acde-47b8-9cf2-c0188a1878c4",
  "show": {
    "entity_id": "5987addb-8e28-43f8-9814-daba8c50dd58",
    "kind": "anime",
    "title": "Example Anime",
    "image_id": "show-poster-image-id"
  },
  "data": {
    "number": 1,
    "titles": [
      {"value": "Season 1", "language": "en", "type": "display"}
    ],
    "overviews": [
      {"value": "The first season.", "language": "en", "type": "overview"}
    ],
    "premiere_date": "2024-01-10",
    "end_date": "2024-03-27",
    "status": "ended",
    "episode_count": 12,
    "aired_episode_count": 12,
    "external_ids": [
      {"provider": "tvdb", "namespace": "season", "value": "123456"}
    ],
    "images": [
      {"id": "season-poster-image-id", "class": "poster", "provider": "tvdb"}
    ]
  },
  "episodes": []
}
```

The `episodes` list MUST have stable documented ordering.

### 6.3 Episode requirements

Both embedded episode entries and `GET /api/v2/episodes/{id}` MUST expose:

- stable episode UUID;
- stable parent season UUID when a season exists;
- parent show UUID and show kind;
- typed provider external IDs;
- localized titles;
- localized overviews/summaries;
- air date;
- runtime;
- provider-native ratings and vote counts;
- still/thumbnail image IDs;
- scheme-aware numbering;
- explicit special status;
- an explicit episode type or a documented derivation with no ambiguity.

Example target shape:

```json
{
  "id": "dceced02-bce7-4cdd-a65c-ac15d345d32e",
  "show": {
    "entity_id": "5987addb-8e28-43f8-9814-daba8c50dd58",
    "kind": "anime",
    "title": "Example Anime"
  },
  "data": {
    "season_id": "dfb7fbd0-acde-47b8-9cf2-c0188a1878c4",
    "titles": [
      {"value": "A New Beginning", "language": "en", "type": "display"},
      {"value": "新しい始まり", "language": "ja", "type": "original"}
    ],
    "overviews": [
      {"value": "The story begins.", "language": "en", "type": "overview"}
    ],
    "numbers": [
      {"scheme": "aired", "season": 1, "number": 3},
      {"scheme": "absolute", "number": 3},
      {"scheme": "tvdb", "season": 1, "number": 3}
    ],
    "is_special": false,
    "episode_type": "regular",
    "air_date": "2024-01-24",
    "runtime_minutes": 24,
    "ratings": [
      {"system": "tmdb", "value": 8.1, "scale_min": 0, "scale_max": 10, "votes": 145}
    ],
    "external_ids": [
      {"provider": "tvdb", "namespace": "episode", "value": "987654"}
    ],
    "images": [
      {"id": "episode-still-image-id", "class": "still", "provider": "tmdb"}
    ]
  }
}
```

### 6.4 Numbering semantics

The current implementation derives episode persistence identity from the first
number in the slice. That makes ordering of `numbers` identity-sensitive.
Before Heya integrates, the service MUST define and test a deterministic
identity and numbering policy.

Required semantics:

- Scheme names MUST be normalized to a documented lowercase vocabulary.
- Provider-specific schemes MUST not be presented as universal canonical order.
- Regular episodes SHOULD have one preferred `aired` season/number pair when
  enough evidence exists.
- Anime episodes SHOULD expose an integer `absolute` number when providers
  support it.
- Specials MUST be explicit; consumers must not need to guess solely from
  season zero.
- If fractional provider numbers are retained, the behavior when mapping to a
  Heya integer episode slot MUST be documented.
- Conflicting number evidence MUST retain provenance and resolve
  deterministically.
- A refresh that changes provider ordering MUST not create a duplicate canonical
  episode UUID.
- Two specials released on the same day MUST remain distinct.
- Season association MUST be based on the selected season scheme, not blindly
  on whichever number happens to be first.

Recommended stable scheme vocabulary:

```text
aired
dvd
absolute
tmdb
tvdb
tvmaze
anidb
```

Other schemes may exist, but their meaning must be documented.

### 6.5 Provider and merge expectations

TV/anime canaries MUST prove that the fields are actually collected and merged,
not merely representable.

At minimum validate:

- TVMaze-rooted conventional show identity;
- TMDB enrichment for artwork, videos, recommendations, ratings and credits;
- TVDB enrichment for season/episode IDs and numbering;
- AniDB-rooted anime identity;
- anime alternate/native titles and absolute numbering;
- language-aware show, season and episode artwork;
- merge behavior when providers disagree about episode count or numbering;
- partial provider failure without erasing previously valid metadata.

### 6.6 TV/anime coverage entries

`coverage/tv.json` currently covers only cast, crew, ratings, and bounded embedded
credits. It MUST be expanded to cover at least:

```text
tv.show.identity
tv.show.localized_titles
tv.show.localized_overviews
tv.show.classification
tv.show.lifecycle
tv.show.networks
tv.show.studios
tv.show.creators
tv.show.external_links
tv.show.videos
tv.show.certifications
tv.show.recommendations
tv.show.images
tv.season.identity
tv.season.metadata
tv.season.images
tv.episode.identity
tv.episode.localized_text
tv.episode.numbering
tv.episode.ratings
tv.episode.images
anime.identity.separate_from_tv
anime.episode.absolute_numbering
anime.episode.specials
```

Equivalent anime-specific coverage entries SHOULD be present where provider
behavior differs from conventional TV.

## 7. Music: blocking artist top-tracks requirement

Heya uses provider top tracks as an artist's "Popular Tracks" rail, matches them
to locally owned recordings, exposes them through its API and Subsonic, and uses
the resulting local rows as playable recommendations.

V2 artist detail currently exposes metrics and similar artists but no ranked
top-track evidence. This MUST be added unless the product explicitly replaces
the feature with a different ranking source before migration.

### 7.1 Acceptable contract shapes

Either of these is acceptable:

1. a bounded `data.top_tracks` projection with a dedicated endpoint for the
   complete list; or
2. a paginated endpoint such as
   `GET /api/v2/entities/{artist_id}/top-tracks`.

Do not hide an unbounded list inside artist detail.

### 7.2 Required fields

Each item MUST expose:

- stable rank within provider/source;
- title;
- provider/source;
- provider track/recording identifier when supplied;
- canonical `recording_entity_id` when reconciliation has resolved it;
- recording MBID when supplied;
- playcount and listener count when supplied;
- informational provider URL when supplied;
- provenance/freshness either per item or through the containing projection.

Example:

```json
{
  "results": [
    {
      "rank": 1,
      "title": "うっせぇわ",
      "provider": "lastfm",
      "recording_entity_id": "optional-recording-uuid",
      "external_ids": [
        {"provider": "musicbrainz", "namespace": "recording", "value": "recording-mbid"}
      ],
      "playcount": 123456789,
      "listeners": 2345678,
      "url": "https://www.last.fm/music/..."
    }
  ],
  "next_cursor": null
}
```

Signed audio preview URLs are not required and SHOULD NOT be made durable.

### 7.3 Top-track behavior

- Ordering MUST be deterministic.
- Refresh MUST replace or version the ranking atomically.
- Duplicate localized/romanized titles SHOULD remain as provider evidence; Heya
  will deduplicate against owned canonical recordings.
- Top-track collection MUST NOT create duplicate recording entities only to make
  every item canonical.
- Unresolved tracks must remain useful by title and external ID.
- Provider failure MUST not silently replace a previously populated list with an
  empty successful list unless the provider explicitly reports an empty list.

### 7.4 Top-track acceptance

Add coverage and canary tests that:

1. resolve an artist with Last.fm evidence;
2. fetch a non-empty ranked list;
3. retain playcount/listener metrics;
4. expose a recording MBID or provider identifier when available;
5. attach `recording_entity_id` when the recording already exists;
6. refresh the artist and prove the list updates atomically;
7. prove artist detail remains bounded.

## 8. Music catalog requirements that must remain stable

The following existing capabilities MUST not regress while adding top tracks:

- artist discovery with hints and safe namesake separation;
- canonical artist names, aliases, sort/native names and disambiguation;
- biographies, lifecycle, areas, genres, tags, links and images;
- group/member relationships;
- similar artists;
- artist-to-release-group discography relations;
- release-group-to-issued-release traversal;
- issued release media and ordered track placements;
- track placement to canonical recording identity;
- labels, barcodes, catalog numbers, ISRCs, credits and durations where supplied;
- lyrics and fingerprint evidence where available;
- deterministic handling of kana/romaji duplicate presentations.

The end-to-end canary MUST traverse:

```text
artist
  -> discography relation
  -> release group
  -> issued release/edition
  -> medium and track placement
  -> recording
```

The service MUST make clear which lists are complete and which are previews.

## 9. Books: series and audiobook decisions

### 9.1 Book-series relationship

Heya currently has local `series_name` and `series_number` fields. V2 book work
and edition documents currently do not expose series membership.

Before Heya begins its book adapter, the team MUST choose one of:

1. implement canonical/book-provider series relationships; or
2. explicitly approve removal of book-series metadata during migration.

Implementation is preferred.

An implemented series relationship SHOULD expose:

- canonical series ID when the service models series identity;
- provider series ID otherwise;
- name and localized names when available;
- position as a string/decimal-capable value, not only an integer;
- provider and provenance;
- work/edition scope.

Example:

```json
{
  "series": [
    {
      "entity_id": "optional-series-uuid",
      "name": "The Expanse",
      "position": "1",
      "provider": "openlibrary"
    }
  ]
}
```

### 9.2 Audiobook discovery

The team MUST document whether audiobook-specific corroboration is part of the
V2 migration target.

If equivalent matching confidence is required, discovery SHOULD accept an
`audiobook`/format hint and use an appropriate provider such as Audible as
identity evidence without collapsing the underlying literary work and edition
model.

If this is intentionally deferred, the handoff must say:

- which audiobook hints V2 accepts;
- which providers participate;
- the expected confidence regression;
- how Heya should present ambiguous audiobook candidates.

### 9.3 Publication capabilities that must remain stable

- book work and book edition identities remain distinct;
- authors are ordered canonical identities, not flattened strings;
- ISBN-10 and ISBN-13 are preserved as namespaced external IDs/evidence;
- work-to-edition relations remain traversable;
- titles, descriptions, subjects, languages, publication dates, publishers,
  format, page count, ratings and cover image IDs remain available;
- manga works and physical volumes remain distinct from ordinary books and from
  each other.

## 10. People and credits

The absence of a person batch endpoint does not block Heya. Current Heya person
pages are lazily loaded, so canonical person IDs embedded in credits plus bounded
parallel person reads are sufficient.

The following MUST remain true:

- every resolved person has a stable canonical UUID;
- cast/crew results carry canonical `person_entity_id` when known;
- provider person ID, department, job, character, order and credit type are not
  lost;
- full credits are available through the paginated projection, not only an
  embedded preview;
- person detail carries names, biography, dates, gender, birthplace,
  known-for department, homepage, popularity, localized biographies and image
  IDs where evidence exists;
- reverse filmography resolves provider-person inputs to the canonical person;
- multiple profile images can be selected/materialized through opaque IDs;
- names alone are never used as person identity.

Add a canary that traverses a movie or TV credit to a canonical person and then
reads reverse filmography.

## 11. Movies

No blocking movie model gap was found, but the handoff MUST prove that the
current projection remains complete for Heya's consumer needs:

- localized titles, overviews and taglines;
- external IDs;
- runtime, budget, revenue and popularity;
- release events, status and certifications;
- original language, spoken languages and origin countries;
- genres and keywords/tags;
- provider-native ratings and vote counts;
- structured studios/production companies;
- videos and homepage/external links;
- full cast and crew with canonical person links where available;
- posters, backdrops, logos and classified alternate artwork;
- collection identity and complete member traversal;
- recommendations with canonical target ID when resolved.

The Matrix/TMDB 603 canary described in `HEYAMEDIA_V2_MIGRATION.md` SHOULD remain
the baseline. It MUST exercise complete paginated credits and an initially
unmaterialized image variant.

## 12. Community skip segments: mandatory ownership decision

Community skip segments are not metadata, but they are an active dependency on
the old service. Heya currently consumes:

```text
movie segments by provider ID and runtime
episode segments by provider ID, season, episode and runtime
```

Candidate results include:

- type: intro, recap, credits, preview, or commercial;
- start and optional end in milliseconds;
- source runtime in milliseconds;
- submission/vote count;
- source: TheIntroDB, SkipMeDB, or AniSkip;
- a `found` distinction between a successful empty result and provider failure.

The recommended ownership is Heya, consistent with the boundary in
`HEYAMEDIA_V2_MIGRATION.md`. The provider clients and cache should be moved into
Heya before cutover.

If the team instead assigns this to HeyaMetadata, V2 MUST provide an equivalent
documented contract and preserve the runtime-aware candidate behavior.

The handoff is blocked until one implementation exists. Acceptance is simple:

1. disable the old heya.media process;
2. run movie, conventional TV, and anime segment workers;
3. verify successful found, successful-not-found, retryable provider error, and
   runtime mismatch behavior;
4. verify no active Heya call still targets the old segment paths.

## 13. OpenAPI and generated client deliverables

The HeyaMetadata handoff MUST include:

- committed OpenAPI document;
- regenerated Go client;
- zero generated drift according to `make check-generated`;
- explicit `200` and `202` response types;
- discriminated kind-specific entity schemas where supported by the generator;
- typed season, episode and top-track resources;
- typed problem responses;
- documented pagination and locale parameters;
- documented auth and provider credential headers;
- examples for discovery, resolution, jobs, images, TV/anime and top tracks.

If entity detail must remain a raw polymorphic envelope, the handoff MUST also
provide stable exported DTOs and a documented decoder keyed by `kind`. Heya
cannot safely build a long-lived adapter against undocumented `interface{}`
payloads.

Required commands:

```sh
make generate-api
make acceptance
make check-generated
```

All MUST pass from a clean worktree.

## 14. Consumer-shaped acceptance suite

Passing unit tests for model packages is necessary but insufficient. Coverage
manifests are executable scope, so omitted capabilities can otherwise appear
green merely because nothing asserts them.

### 14.1 Contract and health

- readiness reports required dependencies accurately;
- OpenAPI and generated client are reachable/current;
- every dynamic success status decodes through typed generated responses;
- problem responses decode consistently;
- version/schema compatibility is exposed.

### 14.2 Identity workflow

- local search hit performs no discovery or resolution;
- miss follows discovery and only the selected candidate is resolved;
- unsafe ambiguity remains unresolved;
- exact ingestible root ID resolves directly;
- supplemental provider ID does not invent a root namespace;
- retry and concurrent resolution produce one entity;
- restart resumes a persisted discovery/job ID.

### 14.3 TV and anime

- conventional TV remains kind `tv_show`;
- anime remains kind `anime` and cannot merge into a TV entity;
- show, season and episode UUIDs are stable across refresh;
- standalone season and episode resources contain all required fields;
- localized overview and poster/still selection works;
- ratings and vote counts survive normalization;
- regular, special, fractional-provider and absolute-number fixtures map
  deterministically;
- two same-day specials remain distinct;
- structural refresh emits an actionable change event.

### 14.4 Music

- namesake artist discovery remains safe;
- full artist-to-recording traversal succeeds;
- issued tracks expose `recording_entity_id` when resolved;
- top tracks are non-empty for the chosen canary and retain metrics;
- a top track links to an existing recording when evidence matches;
- unresolved top tracks do not create duplicate recordings;
- catalog and top-track refreshes are atomic and change-visible.

### 14.5 Publications and people

- work, edition and author IDs remain distinct;
- series relationship or approved absence is asserted;
- audiobook hint behavior is asserted;
- manga work and volume kinds remain distinct;
- credit-to-person and reverse filmography traversal succeeds.

### 14.6 Images

- selection honors language/country fallback;
- opaque IDs remain stable across reads;
- materialized request returns bytes;
- unmaterialized request returns a typed `202`, then bytes after job success;
- season posters, episode stills, person profiles and music/book covers are
  covered;
- provider URLs/credentials do not leak.

### 14.7 Change feed and restart

- start from a documented bootstrap cursor;
- ingest/refresh and receive the change;
- fail local processing and replay without data loss;
- commit only after success;
- replay a page idempotently;
- replace an older cached projection with a higher version;
- follow redirect/merge and tombstone events;
- recover after HeyaMetadata and Heya process restarts.

### 14.8 Cutover proof

The final proof MUST run with old metadata infrastructure disabled:

```text
scanner
  -> local search
  -> discovery/resolution on miss
  -> canonical UUID persistence
  -> kind-specific canonical reads
  -> local Heya read-model persistence
  -> artwork materialization
  -> credits/people and relationship traversal
  -> change-feed refresh
```

It must cover one movie, conventional TV show, anime, music artist/catalog, book,
and person. Segment workers must use their deliberate replacement.

## 15. Handoff artifacts expected from the HeyaMetadata implementer

When the prerequisites are complete, provide all of the following:

1. **Contract summary**
   - final endpoint list;
   - schema and behavior changes;
   - any consciously omitted legacy behavior;
   - compatibility notes.

2. **Generated artifacts**
   - committed OpenAPI;
   - regenerated Go client;
   - successful drift check.

3. **Example payloads**
   - immediate and asynchronous discovery;
   - immediate and asynchronous resolution;
   - terminal job success/failure;
   - one document for every root kind Heya consumes;
   - standalone season and episode;
   - top tracks;
   - image `202` and completed bytes request;
   - change entries for update, structural update, merge/redirect and deletion.

4. **Canary identifiers**
   - stable fixtures or provider IDs for every acceptance domain;
   - required credentials, if any, described without committing secrets.

5. **Test evidence**
   - commands run;
   - passing contract, coverage, domain and acceptance suites;
   - proof that async paths were actually forced;
   - proof that the old metadata service was disabled for cutover tests.

6. **Operational notes**
   - required environment variables;
   - readiness dependencies;
   - expected ports;
   - rate-limit/retry behavior;
   - cursor retention and full-resync procedure;
   - migration/backfill guidance for provider IDs.

7. **Outstanding decisions**
   - book series;
   - audiobook corroboration;
   - segment ownership;
   - any deliberately unsupported field or route.

## 16. What Heya will implement after this handoff

Once the start gates are complete, the Heya-side migration can safely proceed:

1. introduce a configurable HeyaMetadata V2 client and readiness check;
2. replace concrete `*heyamedia.HeyaProvider` coupling with small domain and
   workflow interfaces;
3. add canonical UUID/kind/projection-version persistence and backfill existing
   provider-ID mappings;
4. implement the durable search/discovery/resolution/job state machine;
5. preserve ambiguity for manual scanner decisions;
6. split `tv_show` and `anime` rather than mapping both to the old `tv` kind;
7. decode kind-specific documents and populate Heya's local relational read
   models;
8. traverse full credits, people, discography, releases, tracks, recordings,
   editions, seasons and episodes;
9. replace provider artwork URLs with opaque image materialization;
10. consume the change feed transactionally and retire old stale-refresh logic;
11. migrate or proxy any remaining read endpoints intentionally;
12. remove old provider/enrichment/image-metadata code only after cutover proof.

Heya may begin small non-semantic scaffolding earlier, but it should not freeze
its adapter or database migration around incomplete episodic/top-track contracts.

## 17. Final ready-for-Heya checklist

The HeyaMetadata implementer should check every item before handing the work
back:

### Contract

- [ ] OpenAPI is committed and generated artifacts are current.
- [ ] All legitimate `202` success bodies are typed.
- [ ] Polymorphic entity decoding is documented and stable.
- [ ] Pagination, locale, auth, errors and retry behavior are documented.

### Identity and workflow

- [ ] Search/discovery/resolution ownership is unchanged and tested.
- [ ] Durable resources support restart/resume.
- [ ] Concurrent/retried resolution cannot fork identity.
- [ ] Redirect, merge and deletion behavior is public.

### TV/anime

- [ ] Show-level missing metadata is implemented.
- [ ] Seasons expose overview, status, counts, external IDs and image IDs.
- [ ] Episodes expose localized overview, ratings, stills and external IDs.
- [ ] Numbering and special/type semantics are deterministic.
- [ ] Normalizers and persistence populate the new fields.
- [ ] Coverage catalogs describe the complete contract.
- [ ] TV and anime canaries pass.

### Music

- [ ] Canonical artist top tracks exist.
- [ ] Ranked metrics and recording evidence survive normalization.
- [ ] Existing catalog traversal and namesake safety still pass.

### Books and people

- [ ] Book-series implementation or removal decision is recorded.
- [ ] Audiobook discovery behavior is recorded and tested.
- [ ] Work/edition/author separation remains intact.
- [ ] Person-credit and reverse-filmography traversal passes.

### Images and changes

- [ ] Season posters and episode stills use opaque image IDs.
- [ ] Image materialization `200` and `202` paths pass.
- [ ] Change entries cover child/structural updates.
- [ ] Replay, restart and full-resync behavior is tested.

### Cutover

- [ ] Segment replacement exists and works without old heya.media.
- [ ] Consumer-shaped end-to-end suite passes.
- [ ] Old metadata infrastructure is disabled during the proof.
- [ ] Known omissions and product decisions are written down.

## 18. Definition of ready

HeyaMetadata is ready for the Heya migration when:

- Heya can build a stable adapter entirely from committed public contracts;
- every active Heya metadata domain has sufficient canonical data to populate its
  current local read model or has an approved removal/migration decision;
- TV/anime resources carry complete show, season, episode, numbering, rating and
  image semantics;
- artist top tracks have a canonical, bounded contract;
- asynchronous success responses work through the generated client;
- the change feed can drive reliable transactional invalidation and recovery;
- community segments have a deliberate working owner;
- consumer-shaped acceptance tests pass with the old service disabled;
- no required behavior depends on undocumented provider knowledge, response-body
  fallbacks, unstable URLs, or name/slug-based identity.

At that point the Heya-side implementation can proceed without designing around
known contract holes or preserving the old metadata service as an accidental
fallback.
