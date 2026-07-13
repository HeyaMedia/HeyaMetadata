# Canonical integrity audit

The semantic coverage catalog proves that known representative entities expose
required facts. The canonical integrity audit examines the entire stored graph
for a different class of problem: structural corruption, reconciliation debt,
and deferred provider records that may eventually need canonical targets.

Run the human-readable audit with:

```sh
make audit
```

Machine-readable output is available for monitoring and CI:

```sh
go run ./cmd/heya-metadata --json coverage audit
```

The default command fails only for structural errors. `--strict` also fails for
quality warnings, which is useful once a deployment has an accepted warning
baseline. `--sample-limit` controls how many representative records accompany
each count.

Checks are intentionally domain-aware. Canonical people, for example, are
served from `canonical_people` and do not require `api_documents`; unmaterialized
issued music releases are informational because those entities are resolved
lazily. By contrast, a relation pointing to a target of the wrong kind or a
projection newer than its owning entity is always a structural error.

Current categories include:

- entity, search, detail, identity, and provenance invariants;
- projection-version consistency and freshness;
- missing or failed artwork materialization;
- partial latest provider normalization;
- unresolved music discography clusters and probable release-group duplicates;
- deferred issued music releases and cross-abstraction provider ID reuse.

The audit never mutates or automatically merges canonical entities. Findings
are evidence for domain-specific repair jobs and stronger golden fixtures.
