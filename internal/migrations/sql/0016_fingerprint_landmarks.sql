CREATE TABLE recording_fingerprint_landmarks (
    fingerprint_id uuid NOT NULL REFERENCES recording_fingerprints(id) ON DELETE CASCADE,
    token integer NOT NULL,
    PRIMARY KEY(fingerprint_id, token)
);
CREATE INDEX recording_fingerprint_landmarks_token_idx ON recording_fingerprint_landmarks(token, fingerprint_id);

INSERT INTO recording_fingerprint_landmarks(fingerprint_id,token)
SELECT f.id, band*65536 + get_byte(f.fingerprint,pos+band*2)*256 + get_byte(f.fingerprint,pos+band*2+1)
FROM recording_fingerprints f
CROSS JOIN LATERAL generate_series(0,length(f.fingerprint)-4,16) pos
CROSS JOIN generate_series(0,1) band
WHERE f.state='ready'
ON CONFLICT DO NOTHING;
