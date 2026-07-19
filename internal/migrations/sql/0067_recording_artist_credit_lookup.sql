-- Artist materialization reconciles legacy recording documents that predate
-- explicit recording -> artist_credit relations. Keep that bounded lookup on
-- the embedded authoritative MusicBrainz credit array index-backed.
CREATE INDEX canonical_recordings_artist_credits_gin
    ON canonical_recordings
    USING gin ((document #> '{data,artist_credits}') jsonb_path_ops);
