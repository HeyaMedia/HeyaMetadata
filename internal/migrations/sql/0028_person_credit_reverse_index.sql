CREATE INDEX entity_credit_projection_person_idx
    ON entity_credit_projections (provider, provider_person_id, entity_id, credit_type, credit_order);
