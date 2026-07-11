# Canonical music identity boundaries

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

The first implemented vertical slice is artist. Release-group, release,
recording, release-track, work, and label normalized schemas follow these
boundaries rather than being inferred from the artist projection.
