# Canonical person reconciliation

Person reconciliation has no public HTTP mutation surface. A low-priority
River pass runs every ten minutes and enriches bounded batches of exact-primary-
name people seen under different provider roots. Recently accessed titles are
prioritized within equally stale batches. Provider enrichment records durable
external-ID, biographical, canonical-credit, and filmography evidence.

Pairs are accepted automatically only when independent evidence converges:
the same IMDb/Wikidata person ID from different upstreams, an exact birth date
plus shared filmography, multiple matching filmography credits, or an already
verified two-provider identity corroborated by a third source. Automatic
decisions use the same immutable moderation audit and redirect transaction as
an operator decision. Ambiguous pairs remain in the review queue.

## Candidate evidence

An exact normalized name only selects possible pairs. A proposal requires a
score of at least `0.80` from independent evidence such as an exact birth date,
birth place, shared canonical title credit, overlapping cross-provider
filmography, or provider-owned external links. Names, fuzzy names, and profile
images never authorize a merge. Two different IDs from the same provider are
never accepted automatically, even when their names and credits match.

List the review queue:

```sh
heya-metadata person reconcile list --state proposed
```

Reject a false match without changing either person:

```sh
heya-metadata person reconcile reject \
  --left <uuid> --right <uuid> \
  --actor <operator> --reason <rationale>
```

Accept a verified match while explicitly choosing the permanent UUID:

```sh
heya-metadata person reconcile accept \
  --left <uuid> --right <uuid> --survivor <uuid> \
  --actor <operator> --reason <rationale>
```

Accept and reject are idempotent for the same recorded decision. Conflicting
repeat decisions fail rather than changing history.

## Merge invariants

The accept operation is one Postgres transaction. It:

- locks the candidate and requires both people to be active;
- records the complete pre-merge person, claim, name, and filmography snapshot
  in `moderation_audit_log`;
- preserves the explicitly selected survivor UUID;
- moves external-ID claims, normalized records, canonical title credits,
  provider filmography, names, artwork, provider freshness, and access score;
- combines non-empty person facts without replacing an existing survivor fact
  with an empty value;
- retires the losing entity and records `entity_redirects`;
- translates or supersedes other open candidates involving the retired UUID;
- collapses equivalent cross-provider credit observations only after both rows
  resolve to the same canonical person, while retaining different roles/jobs;
- rebuilds every affected movie/TV/anime detail and credit projection;
- bumps the dependent entities' API/search/provenance versions and emits their
  change-feed entries so Heya refreshes them without rediscovering the title;
- rebuilds the survivor search projection and emits merge/redirect changes.

Requests using a retired person UUID resolve to the active survivor through
both `/api/v2/persons/{id}` and `/api/v2/entities/{id}`. Historical entities,
candidate decisions, and audit snapshots are retained.

## Golden coverage

The people catalog is verified through an in-process instance of the real v2
router:

```sh
heya-metadata coverage verify-people
# or
make golden-people
```

The runner resolves every declared external ID, requires all IDs in a reference
fixture to converge on one active entity, reads the declared projection path,
and verifies expected provider evidence. The internal reconciliation entry also
requires an accepted candidate linked to an immutable accept audit.
