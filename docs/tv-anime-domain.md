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

The intended canonical routes are distinct:

- `/api/v2/tv/...`
- `/api/v2/anime/...`

Likewise, provider discovery will have dedicated TV and Anime entry points. The
generic discovery request still carries a stable `kind` for reusable job,
caching, and ranking infrastructure; dedicated routes inject the kind rather
than asking clients to infer it.

## Provider routing

TV discovery starts with TVDB, TMDB TV, and TVMaze, then uses explicit remote
IDs to unlock supplemental sources. Anime discovery starts with AniDB and anime
mapping authorities, then relates safely to TVDB/TMDB/MyAnimeList/AniList/Kitsu
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
