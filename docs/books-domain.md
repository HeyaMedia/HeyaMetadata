# Canonical books

Books use three canonical boundaries: `author`, `book_work`, and
`book_edition`. A work is the abstract authored text; an edition is one issued
publication with its own publisher, date, language, format, pagination, cover,
and ISBN claims. Google Books volumes supplement editions and never replace the
Open Library work identity merely because titles match.

The normal client flow remains local search, upstream discovery, selected
resolution, then entity read. Use `kind=book_work` for discovery. Useful hints
are `authors`, `year`, and exact `isbns`. Open Library work keys resolve through
the generic `/api/v2/resolutions` endpoint. Editions and authors become locally
searchable and resolvable as they are materialized beneath a work.

Open Library requests are identified and globally rate-limited to three per
second. Provider responses use the shared Redis/S3 observation cache and 48-hour
raw lifecycle; canonical entities do not expire with source blobs. Interactive
ingestion reads at most 50 editions, 20 authors, and 15 Google Books ISBN
supplements per pass. Larger catalog imports must use Open Library data dumps,
not crawl the human-facing API.

`book_ingest_v1` is durable and idempotent. Cover URLs stay in image-candidate
storage; public documents expose only opaque image IDs. A real acceptance run
for `OL27482W` (The Hobbit) produced a work, its author, 50 editions, 66 ISBN
claims, Open Library cover candidates, and nine Google Books volume matches.
