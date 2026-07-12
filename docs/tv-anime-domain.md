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

## Implemented first slice

TVMaze is the initial `tv_show` identity spine. Its canonical document retains
alternate titles, lifecycle, network, external TVDB/IMDb/TVRage IDs, seasons,
full episodes, artwork evidence, and TVMaze numbering. Discovery can verify
known episode titles and numbers before resolution.

AniDB AID is the initial `anime` identity spine. The official daily title dump
is reused for at least 24 hours, then at most the best three candidates are
detail-enriched through the existing half-request-per-second gate. Requests
carry the registered client/client-version where required and the configured
`HEYA_METADATA_ANIDB_USER_AGENT`, which defaults to the old server's
`heya-media/1.0 anidb-titles-sync` value.

Anime detail retains the source episode count while exposing all returned
episodes. Regular, special, credit, trailer, and parody episode numbers remain
separate named schemes. Cowboy Bebop therefore reports 26 conventional
episodes even though supplemental AniDB entries are also retained.

Both kinds reuse River priority, caching, access-frequency refresh,
observations, search, change-feed, and low-level episodic storage primitives.
They have separate canonical tables, jobs, kinds, discovery routes, and public
read routes. Neither can resolve into the other.
