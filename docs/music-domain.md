# Canonical music identity boundaries

The longer-term catalog, ML-analysis, and social evidence plan lives in
[music-evidence-roadmap.md](./music-evidence-roadmap.md). Provider-preview
Chromaprint evidence and LRCLIB lyrics are implemented for release-backed
recordings.

Music is not modeled as a single artist/album/track tree. Provider catalogs
collapse different real-world concepts, so v2 keeps the following canonical
boundaries even when a public projection embeds related records.

## Artist

An artist is a person, group, orchestra, choir, character, or other credited
performing/creating identity. A legal person and each independently credited
stage identity may be separate artists. A band is not merged with its members.
An artist credit such as “Artist A feat. Artist B” is a presentation object and
relationship sequence, not another artist.

Automatic artist identity requires a durable provider assertion:

- the same accepted external provider ID;
- an explicit cross-provider relationship from a retained source record; or
- a reviewed mapping/import whose provenance is retained.

Names, sort names, transliterations, matching biographies, country, dates, or
catalog overlap may rank reconciliation candidates but never auto-merge them.
MusicBrainz artist MBIDs are the initial identity spine because MusicBrainz
preserves artist-credit structure and exposes explicit URL relationships to
many supplemental catalogs. This is precedence for identity bootstrap, not a
claim that MusicBrainz wins every metadata field.

## Release group / album concept

A release group is the abstract album, EP, single, broadcast, or other release
concept. “Abbey Road” as a work in an artist's discography is a release group.
Deluxe editions, regional issues, remasters, digital reissues, and physical
pressings normally remain releases beneath that group.

Provider “album” IDs from Apple, Deezer, Spotify, and similar catalogs are not
assumed to be release-group IDs. They are provider catalog objects until their
edition semantics and cross-provider evidence establish whether they map to a
release group, a specific release, or both through separate claims.

## Release / edition

A release is a specific issued edition distinguished by evidence such as
barcode, catalog number, country, date, label, medium/format, track layout, or
provider storefront availability. MusicBrainz releases and Discogs releases
start here. Two digital services listing apparently identical albums do not
become one release solely from title, date, and track count.

Storefront availability is time-varying evidence attached to a provider catalog
object/release relation. It is not canonical release identity by itself.

## Recording and release track

A recording is a particular captured performance or studio recording. A
release track is that recording's placement on one release/medium, retaining
credited title, artist credit, disc/side, position, duration, and optional
provider track ID. The same recording can appear as many release tracks.

ISRC is a strong identity candidate but not infallible: reused, mistyped, and
version-ambiguous ISRCs go through conflict handling rather than forcing a
merge. Duration/title/audio-fingerprint similarity ranks candidates only.

## Musical work

A musical work is the underlying composition, separate from recordings and
release tracks. Composer/songwriter relations belong to the work when the
source supports that distinction. Classical catalog designations such as opus,
Köchel, BWV, and work movements are retained structurally. Open Opus works are
provider objects until an explicit mapping connects them to a canonical work;
composer-name matching alone is insufficient.

## Label and artist relationships

Labels are canonical organizations with historical names and provider claims.
Band membership, collaboration, supporting-musician, conductor, orchestra,
choir, producer, remixer, and similar relations retain role, time span, credited
name, ordering, and source. Provider relationships without enough evidence to
resolve the target remain provider-scoped normalized relations rather than
creating a guessed canonical entity.

## Merge posture

Normalized records preserve every source claim. Combination is deterministic:

- strong identifiers establish identity, with conflicts quarantined;
- localized names are unioned, while display-name selection is explicit;
- biographies and descriptions remain independently attributable;
- provider genres/tags and popularity metrics keep provider meaning and scale;
- lifecycle dates preserve precision and source rather than inventing a day;
- images retain provider, locale, dimensions, and source URL;
- no provider-global score is presented as a universal rating;
- user overrides may select presentation winners without deleting source facts.

### Cross-script release presentation deduplication

Provider catalog objects remain separate evidence, but the public edition list
must not show the same logical release once in native script and again in a
romanization. Comparison keys use Kagome with its IPA dictionary for Japanese
compound readings, kana-to-romaji conversion, and Unidecode coverage for
Chinese, Thai, Cyrillic, Greek, Hangul, and other scripts. Thus `初夏`, `ショカ`,
and `Shoka` can produce shared comparison evidence without overwriting any
original title.

Cross-provider editions collapse in presentation when barcode matches, or when
romanized/normalized title, compatible year, and compatible track count agree.
Every contributing provider ID/link remains in `edition.sources`. Commercial
SKU annotations such as Deluxe/Expanded/Bonus Track may collapse, while Remix,
Live, Acoustic, Demo, Remaster, Karaoke, and Instrumental remain distinct.
Transliteration is matching evidence only and can never establish canonical
identity by itself.

### Recording evidence

Verified Apple and Deezer track matches may carry a legal 30-second preview.
During the durable release ingestion job, Heya downloads each preview with a
strict HTTPS CDN allowlist and 8 MiB limit, runs `fpcalc -raw`, stores the packed
little-endian Chromaprint sequence on the canonical recording, and immediately
deletes the temporary audio. Signed preview URLs are never copied into
normalized/canonical records, River arguments, or fingerprint evidence. The
exact raw provider response may contain one under the existing 48-hour
observation lifecycle. A stable checksum of the provider, track ID, and
unsigned object path is retained as provenance.

`GET /api/v2/recordings/{heya_id}/fingerprints` returns ready evidence as
`base64-uint32le`, including the algorithm version, hash count, duration,
provider track, checksum, and generation time. Failed permanent previews are
kept as a bounded retry/negative-cache state but are not exposed publicly.

Clients match audio through `POST /api/v2/fingerprint-matches`. A raw
`base64-uint32le` fingerprint is prefiltered through indexed Chromaprint
landmarks, then verified with the full offset-tolerant bit-error matcher.
Clients may also submit the standard compressed Chromaprint form for AcoustID.
The River job combines local and AcoustID evidence, returns existing Heya
recording IDs where known, and otherwise returns a normal MusicBrainz recording
resolution object. Scores rank candidates; they never merge identity.

Submitted fingerprints live only in the short-lived match run, expire after one
hour, and are erased immediately after completion. AcoustID keys may be supplied
with `X-Heya-AcoustID-API-Key`; the key uses the opaque Redis credential handoff
and is never stored in River or Postgres.

The same release job performs LRCLIB's bounded, cached exact-signature lookup
with track, artist, album, and rounded duration. Exact responses run through
the shared Redis/S3 provider cache and observation system before plain and synchronized
forms are stored on the recording. `GET
/api/v2/recordings/{heya_id}/lyrics` returns the LRCLIB record ID, content
checksum, source observation, retrieval time, and lyric forms. A missing lyric
is negative-cached and does not fail release ingestion.

LRCLIB's uncached lookup may itself fan out to external sources and therefore
runs only on an internal, single-worker background queue. It cannot stall
interactive resolution, and no public endpoint can enqueue it.

## Implemented release and recording slice

Artists and release groups are implemented, and MusicBrainz issued releases
now materialize as a separate canonical `release` kind. A release retains its
status, date, country, barcode, packaging, label/catalog assertions, ordered
media, disc IDs, and complete track layout. It is available through
`GET /api/v2/releases/{heya_id}` and direct MusicBrainz release resolution.

Each MusicBrainz recording referenced by a release track materializes once as a
canonical `recording` entity. The release track remains a placement carrying
its own credited title, artist credit, medium position, printed number,
sequence, duration, and MusicBrainz track ID; it references the recording's
Heya ID. Recordings are searchable locally and readable through
`GET /api/v2/recordings/{heya_id}`.

Standalone recordings use the same canonical identity and can be found through
the generic discovery flow with artist, artist-MBID, duration, ISRC, and release
hints. Resolving the selected MusicBrainz MBID runs `recording_ingest_v1` and
retains the recording's disambiguation, artist credits, duration, ISRC
evidence, genres/tags, rating, release appearances, and provider links. A later
release ingestion merges its placement snapshot into that richer document
instead of overwriting it.

ISRC assertions are stored as proposed identity evidence. They never force an
automatic merge; reuse by another recording opens an identity conflict. Apple,
Deezer, Spotify, and Discogs adapters can therefore add storefront tracks and
editions without collapsing release tracks into recordings or treating their
album IDs as release-group identities.

### Supplemental issued-release evidence

Issued MusicBrainz releases probe the free iTunes Search API using artist and
title, Deezer by UPC lookup, and Discogs by authenticated barcode search.
Search results are not identities. iTunes candidates require matching artist,
release year, edition title, complete track count, and strong track-layout
agreement. Barcode candidates require the same normalized barcode and complete
track count plus compatible title/year. Provider failures remain partial
enrichment and do not fail the MusicBrainz release spine.

Verified provider editions appear in `data.sources` and become resolvable
external claims for that canonical release. Release tracks retain every matched
provider track in `track.sources`. Matching prefers ISRC and otherwise requires
the already-verified edition plus compatible disc, sequence, normalized title,
and duration. This enriches placements without creating Apple/Deezer/Discogs
recording entities or promoting title similarity into recording identity.
