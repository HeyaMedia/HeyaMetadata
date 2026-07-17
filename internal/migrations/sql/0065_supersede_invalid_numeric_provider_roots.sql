-- Upstream remote-ID fields occasionally contain provider presentation slugs
-- such as "1931-disney-s-adventures-of-the-gummi-bears". These values are not
-- callable provider identities and must never remain accepted ingestion roots.
UPDATE external_id_claims
SET state = 'superseded'
WHERE state = 'accepted'
  AND normalized_value !~ '^[1-9][0-9]*$'
  AND (
    (provider = 'tmdb' AND namespace IN ('movie', 'tv')) OR
    (provider = 'tvmaze' AND namespace = 'show') OR
    (provider = 'anidb' AND namespace = 'anime')
  );
