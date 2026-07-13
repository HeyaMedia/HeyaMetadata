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

Likewise, provider discovery will have dedicated TV and Anime entry points. The
generic discovery request still carries a stable `kind` for reusable job,
caching, and ranking infrastructure; dedicated routes inject the kind rather
than asking clients to infer it.

## Provider routing

TV discovery starts with TVMaze, then uses explicit remote IDs to unlock TVDB,
TMDB TV, and supplemental sources. Anime discovery starts with AniDB's official
daily title dump and detail API, then uses mapping authorities to relate safely
to TVDB/TMDB/MyAnimeList/AniList/Kitsu
identities where explicit mappings exist. A title resemblance is candidate
evidence only.

Anime ranking gives first-class weight to native/romanized/English titles,
format, start year, episode count, season, source material, studio, and known
episode or release titles. TV ranking emphasizes original/localized title,
premiere year, country, network, status, and known season/episode titles.

## Numbering

Anime episode numbering is a projection over provider assertions. Absolute,
seasonal, cour, aired, DVD, and specials numbering remain named schemes with
provenance. TV seasons and episodes use the same scheme-aware primitive, but the
default TV projection does not inherit anime-specific assumptions.

## Implemented multi-source slice

TVMaze is the initial `tv_show` identity spine. Its canonical document retains
alternate titles, lifecycle, network, external TVDB/IMDb/TVRage IDs, seasons,
full episodes, artwork evidence, and TVMaze numbering. Discovery can verify
known episode titles and numbers before resolution.

An accepted TVMaze `tvdb.series` claim unlocks TVDB extended series evidence.
An accepted IMDb title claim is resolved through TMDB's external-ID index and
then unlocks TMDB TV detail plus each conventional season. The deterministic
mixer keeps TVMaze scalar priority while unioning titles, classifications,
artwork, localized text, organizations, links, videos, certifications,
recommendations, ratings, and explicit IDs. Episodes match within an authority
by typed external ID or exact numbering. Conventional cross-authority aired
numbers require corroborating date or title evidence; the weaker final fallback
requires both the same air date and normalized title. Absolute anime order can
match across authorities even when their aired orders conflict. The result
retains `aired`, `tvmaze`, `tvdb`, `tmdb`, and available `absolute` numbers.
Season zero is retained, and specials have explicit `is_special` and
`episode_type` values.

AniDB AID is the initial `anime` identity spine. The official daily title dump
is reused for at least 24 hours, then at most the best three candidates are
detail-enriched through the existing half-request-per-second gate. Requests
carry the registered client/client-version where required and the configured
`HEYA_METADATA_ANIDB_USER_AGENT`, which defaults to the old server's
`heya-media/1.0 anidb-titles-sync` value.

Anime detail retains the source episode count while exposing all returned
episodes. Regular episodes expose canonical `aired` season one and integer
`absolute` evidence. Special, credit, trailer, and parody entries are explicit
typed season-zero resources and retain their AniDB scheme. Cowboy Bebop
therefore reports 26 conventional episodes even though supplemental AniDB
entries are also retained.

Season and episode persistence first resolves typed provider child IDs, then a
deterministic numbering priority independent of JSON slice order. Refreshes
therefore retain child UUIDs even when provider ordering changes. Season posters
and episode stills are materialized as opaque image IDs owned by their child
resource rather than leaking upstream URLs or pretending they are show images.
Within the preferred `aired` scheme, TVMaze wins for conventional TV and AniDB
wins for anime; supplemental provider order remains present in `numbers[]`.

The cached Fribb anime-lists mapping dump is the explicit AniDB-to-MAL,
AniList, and TVDB bridge. TVDB enrichment is restricted to the mapped season;
`episode_offset.tvdb` translates split-cour numbering without changing the
original TVDB number. AniDB resource groups that contain several IDs in the
same namespace remain ambiguous evidence and are not accepted as canonical
claims. The mapping authority supplies the selected MAL/AniList identity.

All contributing normalized records are persisted independently and the
canonical projection records each provider in freshness and provenance. Raw
TVDB, TMDB, AniDB, TVMaze, and mapping responses use the 48-hour lifecycle
tier. Request-scoped TMDB and TVDB keys flow to episodic River jobs through the
same opaque Redis credential references as movie ingestion.

Both kinds reuse River priority, caching, access-frequency refresh,
observations, search, change-feed, and low-level episodic storage primitives.
They have separate canonical tables, jobs, kinds, discovery routes, and public
read routes. Neither can resolve into the other.
