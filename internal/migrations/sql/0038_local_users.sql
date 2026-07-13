CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    role text NOT NULL DEFAULT 'user',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (username = lower(username)),
    CHECK (username ~ '^[a-z0-9][a-z0-9_.-]{1,62}[a-z0-9]$'),
    CHECK (password_hash LIKE '$argon2id$%'),
    CHECK (role IN ('user', 'moderator', 'trusted', 'admin'))
);
