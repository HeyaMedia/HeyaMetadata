UPDATE entities e SET kind='manga_volume'
WHERE kind='manga' AND EXISTS (
    SELECT 1 FROM external_id_claims c
    WHERE c.entity_id=e.id AND c.provider='openlibrary' AND c.namespace='work'
);
UPDATE entity_slugs s SET kind='manga_volume'
WHERE kind='manga' AND EXISTS (SELECT 1 FROM entities e WHERE e.id=s.entity_id AND e.kind='manga_volume');
UPDATE external_id_claims SET entity_kind='manga_volume'
WHERE entity_kind='manga' AND provider='openlibrary' AND namespace='work';
UPDATE normalized_records SET entity_kind='manga_volume'
WHERE entity_kind='manga' AND provider='openlibrary';
UPDATE search_entities SET kind='manga_volume'
WHERE kind='manga' AND EXISTS (SELECT 1 FROM entities e WHERE e.id=search_entities.entity_id AND e.kind='manga_volume');
UPDATE api_documents d SET document=jsonb_set(document,'{kind}',to_jsonb('manga_volume'::text))
WHERE EXISTS (SELECT 1 FROM entities e WHERE e.id=d.entity_id AND e.kind='manga_volume');
UPDATE canonical_book_works c SET document=jsonb_set(document,'{kind}',to_jsonb('manga_volume'::text))
WHERE EXISTS (SELECT 1 FROM entities e WHERE e.id=c.entity_id AND e.kind='manga_volume');

UPDATE entities e SET kind='comic_volume'
WHERE kind='comic' AND EXISTS (
    SELECT 1 FROM external_id_claims c
    WHERE c.entity_id=e.id AND c.provider='openlibrary' AND c.namespace='work'
);
UPDATE entity_slugs s SET kind='comic_volume'
WHERE kind='comic' AND EXISTS (SELECT 1 FROM entities e WHERE e.id=s.entity_id AND e.kind='comic_volume');
UPDATE external_id_claims SET entity_kind='comic_volume'
WHERE entity_kind='comic' AND provider='openlibrary' AND namespace='work';
UPDATE normalized_records SET entity_kind='comic_volume'
WHERE entity_kind='comic' AND provider='openlibrary';
UPDATE search_entities SET kind='comic_volume'
WHERE kind='comic' AND EXISTS (SELECT 1 FROM entities e WHERE e.id=search_entities.entity_id AND e.kind='comic_volume');
UPDATE api_documents d SET document=jsonb_set(document,'{kind}',to_jsonb('comic_volume'::text))
WHERE EXISTS (SELECT 1 FROM entities e WHERE e.id=d.entity_id AND e.kind='comic_volume');
UPDATE canonical_book_works c SET document=jsonb_set(document,'{kind}',to_jsonb('comic_volume'::text))
WHERE EXISTS (SELECT 1 FROM entities e WHERE e.id=c.entity_id AND e.kind='comic_volume');

ALTER TABLE book_ingestion_runs DROP CONSTRAINT book_ingestion_runs_entity_kind_check;
UPDATE book_ingestion_runs SET entity_kind=CASE entity_kind
    WHEN 'manga' THEN 'manga_volume'
    WHEN 'comic' THEN 'comic_volume'
    ELSE entity_kind END;
ALTER TABLE book_ingestion_runs ADD CONSTRAINT book_ingestion_runs_entity_kind_check
    CHECK (entity_kind IN ('book_work','manga_volume','comic_volume'));

CREATE TABLE canonical_manga (
    entity_id uuid PRIMARY KEY REFERENCES entities(id),
    merge_version text NOT NULL,
    source_fingerprint text NOT NULL,
    document jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE manga_ingestion_runs (
    river_job_id bigint PRIMARY KEY,
    kitsu_manga_id text NOT NULL,
    entity_id uuid REFERENCES entities(id),
    state text NOT NULL CHECK(state IN ('working','completed','failed')),
    error text,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);
