# Television and anime domain boundary

Television and anime are separate canonical kinds and public API families:

- `tv_show` owns conventional series, seasons, episodes, specials, networks,
  air schedules, and provider numbering.
- `anime` owns anime titles, cours, seasons/parts, episodes, specials, OVAs,
  ONAs, movies attached to franchises, studios, source material, and alternate
  absolute/airing/DVD numbering.

They may reuse low-level date, credit, image, external-ID, and episode utilities.
They do not share a canonical show entity or use an `is_anime` flag. A cross-kind
relationship may state that a TV adaptation and an anime entity correspond, but
that relationship never collapses their identities.

## Public surfaces

The canonical routes are distinct:

- `POST /api/v2/tv/discoveries`
- `GET /api/v2/tv/shows/{id}`
- `POST /api/v2/anime/discoveries`
- `GET /api/v2/anime/{id}`

The generic discovery request carries a stable `kind`; dedicated routes simply
inject that kind rather than changing the identity model.

## Provider reconciliation is private

Clients submit every identifier and fact they have. HeyaMetadata privately
selects sources, crosswalks identities, reconciles conflicts, and chooses merge
behavior. No client may depend on a particular source, source order, or
provider-specific resolution request.

Title resemblance is candidate evidence only. Canonical identity is returned
as a Heya UUID, and conflicts remain opaque reviewable candidates.

### Anime series and season identity

AniDB commonly assigns a separate AID to every season or cour, while TVDB,
TMDB, and IMDb may use one identifier for the whole multi-season series. Those
different granularities are not interchangeable. An AniDB entry mapped to
TVDB season 2 or later—or to a nonzero episode offset within season 1—keeps its
AniDB, MAL, AniList, and provider-season identity, but cannot claim the
series-wide TVDB, TMDB, or IMDb identifier.

The Anime Lists bridge explicitly anchors those broad identifiers to the TVDB
series and records them as shared episodic-series evidence. Reverse TVDB
lookup deterministically chooses the season-one or unscoped AniDB entry,
regardless of mapping-dump order. Once that root exists, shared identifiers
resolve to it; refreshing a later season cannot steal them. TVDB and Fanart
payloads normalized for a later AniDB season use season-scoped normalization
and identity keys even though their upstream response covers the full series.

For example, Attack on Titan AniDB `9541`, IMDb `tt2560140`, TMDB `1429`, and
TVDB `267440` converge on the 2013 root entity. AniDB `10944` remains the 2017
season-two entity and conflicts when incorrectly combined with those broad
series identifiers.

## Numbering

Anime episode numbering is a projection over provider assertions. Absolute,
seasonal, cour, aired, DVD, and specials numbering remain named schemes with
provenance. TV seasons and episodes use the same scheme-aware primitive, but the
default TV projection does not inherit anime-specific assumptions.

## Canonical guarantees

The combined document retains alternate and localized titles, lifecycle,
networks/studios, complete seasons and episodes, artwork, credits, ratings,
videos, recommendations, links, classifications, provenance, and explicit
numbering evidence when supplied by applicable sources.

Season and episode persistence is deterministic and independent of source
payload order. Refreshes retain child UUIDs. Season posters and episode stills
are materialized as opaque image IDs owned by their child resource rather than
leaking upstream URLs or pretending they are show images.

Regular anime episodes can expose canonical `aired` and integer `absolute`
evidence. Specials and other non-regular entries remain explicit typed
season-zero resources. Conflicting mappings remain ambiguous rather than being
silently accepted as canonical claims.

All contributing normalized records are persisted independently, and the
canonical projection retains source freshness and provenance. Request-scoped
provider credentials remain private to HeyaMetadata workers.

Both kinds reuse River priority, caching, access-frequency refresh,
observations, search, change-feed, and low-level episodic storage primitives.
They have separate canonical tables, jobs, kinds, discovery routes, and public
read routes. Neither can resolve into the other.
