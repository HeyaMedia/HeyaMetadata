ALTER TABLE canonical_people
    ADD COLUMN biographies jsonb NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE person_reconciliation_candidates (
    left_person_id uuid NOT NULL REFERENCES canonical_people(entity_id) ON DELETE CASCADE,
    right_person_id uuid NOT NULL REFERENCES canonical_people(entity_id) ON DELETE CASCADE,
    score double precision NOT NULL CHECK(score>=0 AND score<=1),
    reasons jsonb NOT NULL,
    state text NOT NULL DEFAULT 'proposed' CHECK(state IN('proposed','accepted','rejected')),
    first_observed_at timestamptz NOT NULL DEFAULT now(),
    last_observed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(left_person_id,right_person_id),
    CHECK(left_person_id<right_person_id)
);
CREATE INDEX person_reconciliation_score_idx
    ON person_reconciliation_candidates(state,score DESC,last_observed_at DESC);
