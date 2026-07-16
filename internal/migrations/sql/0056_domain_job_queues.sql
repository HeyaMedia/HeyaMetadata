-- Give each metadata domain independent River capacity. Do not move running
-- jobs during a rolling deployment; the old worker may finish those in place.
UPDATE river_job
SET queue = 'music'
WHERE state IN ('available', 'pending', 'retryable', 'scheduled')
  AND kind IN (
    'artist_ingest_v1',
    'artist_catalog_sync_v1',
    'release_group_ingest_v1',
    'release_ingest_v1',
    'recording_ingest_v1',
    'recording_evidence_refresh_v1',
    'musical_work_ingest_v1',
    'fingerprint_match_v1'
  );

UPDATE river_job
SET queue = 'movie'
WHERE state IN ('available', 'pending', 'retryable', 'scheduled')
  AND kind = 'movie_ingest_v1';

UPDATE river_job
SET queue = 'tv'
WHERE state IN ('available', 'pending', 'retryable', 'scheduled')
  AND kind = 'tv_show_ingest_v1';

UPDATE river_job
SET queue = 'anime'
WHERE state IN ('available', 'pending', 'retryable', 'scheduled')
  AND kind = 'anime_ingest_v1';

UPDATE river_job
SET queue = 'books'
WHERE state IN ('available', 'pending', 'retryable', 'scheduled')
  AND kind IN ('book_ingest_v1', 'manga_ingest_v1');

-- Older discovery args contain only the request hash. Persist the kind while
-- moving them so retries remain self-describing after this migration.
UPDATE river_job AS job
SET queue = CASE
      WHEN run.kind IN ('artist', 'release_group', 'release', 'recording', 'musical_work') THEN 'music'
      WHEN run.kind = 'movie' THEN 'movie'
      WHEN run.kind IN ('tv_show', 'season', 'episode') THEN 'tv'
      WHEN run.kind = 'anime' THEN 'anime'
      WHEN run.kind IN ('book_work', 'manga', 'manga_volume', 'comic_volume') THEN 'books'
      ELSE job.queue
    END,
    args = jsonb_set(job.args, '{media_kind}', to_jsonb(run.kind), true)
FROM discovery_runs AS run
WHERE job.kind = 'discovery_search_v1'
  AND job.id = run.river_job_id
  AND job.state IN ('available', 'pending', 'retryable', 'scheduled');
