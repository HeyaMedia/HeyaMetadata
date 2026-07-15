ALTER TABLE change_cursor
    ADD COLUMN stream_id uuid;

UPDATE change_cursor
SET stream_id = gen_random_uuid()
WHERE stream_id IS NULL;

ALTER TABLE change_cursor
    ALTER COLUMN stream_id SET DEFAULT gen_random_uuid(),
    ALTER COLUMN stream_id SET NOT NULL;
