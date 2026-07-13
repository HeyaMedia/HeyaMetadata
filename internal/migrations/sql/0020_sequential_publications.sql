ALTER TABLE book_ingestion_runs
    ADD COLUMN entity_kind text NOT NULL DEFAULT 'book_work';

ALTER TABLE book_ingestion_runs
    ADD CONSTRAINT book_ingestion_runs_entity_kind_check
    CHECK (entity_kind IN ('book_work', 'manga', 'comic'));
