CREATE INDEX change_log_public_sequence_idx
    ON change_log (sequence DESC)
    WHERE scope = 'public';
