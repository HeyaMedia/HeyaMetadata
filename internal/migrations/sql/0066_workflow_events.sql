-- Workflow completion feed. Discovery runs (and future async workflows) finish
-- without necessarily touching a canonical entity, so the public change feed
-- never announces them; consumers that parked a continuation on a workflow had
-- to poll every pending run individually. This mirrors the change feed's
-- outbox -> sequencer -> gap-free log shape with its own stream identity.
-- change_outbox cannot be reused: its entity_id references entities(id), and
-- workflow ids are not entity ids.

CREATE TABLE workflow_event_outbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_kind text NOT NULL,
    workflow_id uuid NOT NULL,
    state text NOT NULL CHECK (state IN ('completed', 'failed')),
    completed_at timestamptz NOT NULL,
    committed_at timestamptz NOT NULL DEFAULT now(),
    sequenced_at timestamptz
);
CREATE INDEX workflow_event_outbox_pending_idx
    ON workflow_event_outbox (committed_at, id)
    WHERE sequenced_at IS NULL;

CREATE TABLE workflow_event_cursor (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    stream_id uuid NOT NULL DEFAULT gen_random_uuid(),
    last_sequence bigint NOT NULL DEFAULT 0
);
INSERT INTO workflow_event_cursor DEFAULT VALUES;

CREATE TABLE workflow_event_log (
    sequence bigint PRIMARY KEY,
    outbox_id uuid NOT NULL UNIQUE REFERENCES workflow_event_outbox(id),
    workflow_kind text NOT NULL,
    workflow_id uuid NOT NULL,
    state text NOT NULL,
    completed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
