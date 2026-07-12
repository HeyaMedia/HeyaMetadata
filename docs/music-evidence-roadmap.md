# Gold-tier music evidence roadmap

Heya music metadata is built from independently refreshable evidence layers.
No catalog, fingerprint service, ML model, or user action owns canonical
identity by itself.

## Evidence layers

### Catalog and editorial evidence

MusicBrainz remains the initial artist, release-group, release, and recording
spine. Discogs, free iTunes Search, Deezer, TIDAL, Qobuz, Amazon, Bandcamp,
KKBOX, QQ Music, and NetEase may contribute storefront IDs, availability,
credits, edition facts, artwork, previews, and links when an official or
otherwise explicitly permitted integration exists.

The AudioDB, Genius, Wikidata, Last.fm, and similar knowledge sources provide
editorial facts, relationships, popularity, and discovery signals. LRCLIB is a
separate lyrics-evidence provider. Lyrics retain provider, revision, language,
plain/synchronized form, licensing/attribution requirements, and retrieval
time; they are not copied between sources as unattributed text.

Provider availability is territorial and time varying. A link-out assertion is
therefore `(provider, catalog object, territory, observed_at, availability)`
attached to a release or recording—not a permanent field on its identity.

### Audio identity evidence

The media server may upload a Chromaprint with duration for a local track. The
metadata service stores the fingerprint as a derived observation and attempts:

1. exact/strong AcoustID lookup;
2. exact matches against known full-track fingerprints;
3. offset-consensus partial matching against fingerprints derived from legal
   provider previews;
4. candidate ranking using duration, title, artist, release layout, ISRC, and
   existing provider assertions.

A fingerprint match proposes or strengthens a recording mapping. It never
silently merges two canonical recordings when provider IDs or ISRC evidence
conflict. Conflicts remain reviewable and the submitted fingerprint stays tied
to its provenance.

Chromaprint sequences should use an offset-aware inverted index rather than an
embedding/vector database. Preview-to-full-track matching compares overlapping
hash windows and votes on a stable time offset. Store the compact fingerprint
and derived index terms, not provider preview audio, unless provider terms
explicitly permit retaining the audio.

### ML evidence

Media servers may upload versioned audio-analysis output such as BPM, musical
key, loudness, energy, valence/mood, danceability, speech/instrumental/vocal
probabilities, genres, instrumentation, embeddings, and structural segments.
Every assertion includes:

- the Heya recording ID or unresolved fingerprint reference;
- model family, semantic version, feature schema, and inference parameters;
- analyzed duration/range and source fingerprint checksum;
- values plus calibrated confidence;
- client/server provenance and observation time.

ML features never overwrite catalog facts. Multiple model versions coexist
until a projection policy selects the current presentation. Embeddings use a
model-specific ANN index; scalar features use normal indexed columns. This
supports similarity radio and playlist generation without claiming that an ML
neighbor is the same recording.

### User and social evidence

Playback state, watch state, ratings, lists, playlists, skips, replays, and
shares are user-domain events—not metadata provider observations. Raw private
events remain access-controlled. Public recommendations and popularity use
consented, aggregated, abuse-resistant signals with minimum cohort sizes and
decay.

Shared pages resolve to canonical Heya IDs and present localized metadata plus
territory-aware provider links. URLs and slugs are presentation aliases; the
opaque Heya ID remains the durable reference when a title or artist name
changes.

## Ingestion boundaries

Suggested media-server surfaces:

- `POST /api/v2/recording-fingerprint-observations`
- `POST /api/v2/recording-analysis-observations`
- batch variants with idempotency keys and compressed request bodies;
- a job/status resource for unresolved audio identity;
- explicit consent/account scopes for any playback, list, or playlist sync.

Uploads are content-addressed and retry-safe. River separates interactive
unknown-track identification from low-priority re-analysis and catalog
backfills. Small derived facts stay hot in Postgres/Redis; raw or large
versioned artifacts use bounded object storage retention where appropriate.

## Provider sequence

1. Standalone recording discovery and ingestion.
2. AcoustID lookup plus client Chromaprint upload and conflict-safe mapping.
3. LRCLIB synchronized/plain lyrics evidence.
4. TIDAL official catalog integration once application credentials are set up.
5. Qobuz, TheAudioDB, and Genius after verifying access and content terms.
6. Amazon, Bandcamp, KKBOX, QQ Music, and NetEase only through stable,
   permitted interfaces; scraping private endpoints is not a foundation for a
   durable metadata service.
7. Versioned ML analysis uploads and similarity/playlist projections.
8. Consent-driven playback/list/playlist sync and public link-out pages.

This ordering builds recording identity before adding expensive dependent
features and keeps Heya useful even when a commercial catalog disappears or
changes access policy.
