CREATE TABLE artist_top_tracks (
    artist_entity_id uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    provider text NOT NULL,
    rank integer NOT NULL CHECK (rank > 0),
    title text NOT NULL,
    provider_track_id text NOT NULL DEFAULT '',
    recording_mbid text NOT NULL DEFAULT '',
    playcount bigint NOT NULL DEFAULT 0 CHECK (playcount >= 0),
    listeners bigint NOT NULL DEFAULT 0 CHECK (listeners >= 0),
    url text NOT NULL DEFAULT '',
    source_observation_id uuid REFERENCES provider_observations(id),
    observed_at timestamptz NOT NULL,
    projection_version bigint NOT NULL,
    PRIMARY KEY (artist_entity_id, provider, rank)
);

CREATE TABLE artist_top_track_snapshots (
    artist_entity_id uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    provider text NOT NULL,
    item_count integer NOT NULL CHECK (item_count >= 0),
    reported_total integer NOT NULL CHECK (reported_total >= 0),
    source_observation_id uuid REFERENCES provider_observations(id),
    observed_at timestamptz NOT NULL,
    projection_version bigint NOT NULL,
    PRIMARY KEY (artist_entity_id, provider)
);

CREATE INDEX artist_top_tracks_recording_mbid_idx
    ON artist_top_tracks (recording_mbid)
    WHERE recording_mbid <> '';
