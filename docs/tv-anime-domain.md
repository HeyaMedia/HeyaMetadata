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

Internally, TMDB is the preferred screen root for movies, conventional TV, and
anime. TVDB, TVMaze, AniDB, Anime Lists, and Fanart enrich that root. TVMaze or
AniDB may root an entity only after TMDB search and exact identifier crosswalks
produce no TV result. Root promotion is UUID-preserving, so discovering a TMDB
claim for an older fallback-rooted entity never changes the Heya identity.

### Anime series and season identity

AniDB commonly assigns a separate AID to every season or cour, while TMDB,
TVDB, and IMDb normally identify the whole multi-season series. Those
granularities are preserved without turning each AniDB entry into a competing
show root. The TMDB TV identity owns the canonical anime entity; Anime Lists
maps season/cour-specific AniDB, MAL, and AniList identifiers onto canonical
season resources. TheXEM translates the TVDB episode offsets in those mappings
to the canonical cour/season that actually owns them. The season-one or
unscoped mapping may also corroborate the series identity.

When TMDB genuinely has no corresponding series, an AniDB-rooted fallback may
retain its narrower entity boundary. If a TMDB crosswalk appears later, the
existing Heya UUID is promoted and the narrower identifiers become season
evidence. TVDB and Fanart payloads remain independently normalized and cannot
steal a broad series claim from its canonical root.

For example, Attack on Titan AniDB `9541`, IMDb `tt2560140`, TMDB `1429`, and
TVDB `267440` converge on the 2013 series entity. AniDB `10944` is retained as
season-two evidence under that same Heya anime rather than owning the broad
IMDb/TMDB/TVDB claims.

## Numbering

Anime episode numbering is a projection over provider assertions. Absolute,
seasonal, cour, aired, DVD, and specials numbering remain named schemes with
provenance. TV seasons and episodes use the same scheme-aware primitive, but the
default TV projection does not inherit anime-specific assumptions.

TheXEM is the private structural authority when an anime is flattened by its
screen providers. 86 is represented by TMDB and TVDB as one 23-episode regular
season, while TheXEM maps those episodes to an 11-episode first cour and a
12-episode second cour. HeyaMetadata therefore exposes Specials, Season 1, and
Season 2 as three canonical season resources. Episode 12 retains TMDB/TVDB
`1x12` evidence while its canonical `aired` address is `2x1`. Clients use only
the Heya season and episode UUIDs and need no knowledge of that reconciliation.

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
