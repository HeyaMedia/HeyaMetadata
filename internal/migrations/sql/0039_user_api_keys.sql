CREATE TABLE api_keys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name text NOT NULL,
    key_prefix text NOT NULL UNIQUE,
    key_hash bytea NOT NULL UNIQUE,
    scopes text[] NOT NULL DEFAULT ARRAY[]::text[],
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    expires_at timestamptz,
    revoked_at timestamptz,
    CHECK (char_length(name) BETWEEN 1 AND 64),
    CHECK (name = btrim(name)),
    CHECK (key_prefix ~ '^heya_v2_[A-Za-z0-9_-]{12}$'),
    CHECK (octet_length(key_hash) = 32),
    CHECK (expires_at IS NULL OR expires_at > created_at)
);

CREATE INDEX api_keys_active_user_idx
    ON api_keys (user_id, created_at DESC)
    WHERE revoked_at IS NULL;
