CREATE TABLE episodic_seasons (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    show_entity_id uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    show_kind text NOT NULL CHECK (show_kind IN ('tv_show', 'anime')),
    season_number integer NOT NULL,
    document jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (show_entity_id, season_number)
);
CREATE INDEX episodic_seasons_show_idx ON episodic_seasons (show_entity_id, season_number);

CREATE TABLE episodic_episodes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    show_entity_id uuid NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    show_kind text NOT NULL CHECK (show_kind IN ('tv_show', 'anime')),
    season_id uuid REFERENCES episodic_seasons(id) ON DELETE SET NULL,
    identity_key text NOT NULL,
    document jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (show_entity_id, identity_key)
);
CREATE INDEX episodic_episodes_show_idx ON episodic_episodes (show_entity_id);
CREATE INDEX episodic_episodes_season_idx ON episodic_episodes (season_id) WHERE season_id IS NOT NULL;

INSERT INTO episodic_seasons (show_entity_id, show_kind, season_number, document)
SELECT d.entity_id, e.kind, (season->>'number')::integer, season
FROM api_documents d
JOIN entities e ON e.id=d.entity_id AND e.kind IN ('tv_show','anime') AND e.deleted_at IS NULL
CROSS JOIN LATERAL jsonb_array_elements(COALESCE(d.document->'data'->'seasons','[]'::jsonb)) season
WHERE d.document_kind='detail' AND season ? 'number'
ON CONFLICT (show_entity_id, season_number) DO UPDATE SET document=EXCLUDED.document, updated_at=now();

INSERT INTO episodic_episodes (show_entity_id, show_kind, season_id, identity_key, document)
SELECT d.entity_id, e.kind, s.id,
       COALESCE(NULLIF(numbering->>'scheme',''),'provider') || ':' ||
       COALESCE(numbering->>'season','0') || ':' ||
       COALESCE(numbering->>'number', episode->>'provider_id'),
       episode
FROM api_documents d
JOIN entities e ON e.id=d.entity_id AND e.kind IN ('tv_show','anime') AND e.deleted_at IS NULL
CROSS JOIN LATERAL jsonb_array_elements(COALESCE(d.document->'data'->'episodes','[]'::jsonb)) episode
LEFT JOIN LATERAL (SELECT value FROM jsonb_array_elements(COALESCE(episode->'numbers','[]'::jsonb)) WITH ORDINALITY n(value, ord) ORDER BY ord LIMIT 1) first_number ON true
LEFT JOIN LATERAL (SELECT first_number.value) picked(numbering) ON true
LEFT JOIN episodic_seasons s ON s.show_entity_id=d.entity_id AND s.season_number=COALESCE((numbering->>'season')::integer,0)
WHERE d.document_kind='detail'
ON CONFLICT (show_entity_id, identity_key) DO UPDATE SET season_id=EXCLUDED.season_id, document=EXCLUDED.document, updated_at=now();
