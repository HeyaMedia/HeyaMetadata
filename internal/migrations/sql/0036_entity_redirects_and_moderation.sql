CREATE TABLE moderation_audit_log (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_kind text NOT NULL,
    action text NOT NULL,
    actor text NOT NULL,
    reason text NOT NULL,
    subject_ids uuid[] NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX moderation_audit_subject_idx
    ON moderation_audit_log USING gin(subject_ids);

CREATE TABLE entity_redirects (
    retired_entity_id uuid PRIMARY KEY REFERENCES entities(id),
    survivor_entity_id uuid NOT NULL REFERENCES entities(id),
    entity_kind text NOT NULL,
    audit_log_id uuid NOT NULL REFERENCES moderation_audit_log(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK(retired_entity_id<>survivor_entity_id)
);
CREATE INDEX entity_redirects_survivor_idx
    ON entity_redirects(survivor_entity_id);

ALTER TABLE person_reconciliation_candidates
    DROP CONSTRAINT person_reconciliation_candidates_state_check,
    ADD CONSTRAINT person_reconciliation_candidates_state_check
        CHECK(state IN('proposed','accepted','rejected','superseded')),
    ADD COLUMN decided_at timestamptz,
    ADD COLUMN decided_by text,
    ADD COLUMN decision_reason text,
    ADD COLUMN survivor_person_id uuid REFERENCES canonical_people(entity_id),
    ADD COLUMN audit_log_id uuid REFERENCES moderation_audit_log(id);
