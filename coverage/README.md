# Semantic coverage catalog

This directory records product capabilities that must survive the clean-slate
rewrite. It does not describe the old API shape.

Each entry states:

- the logical fact, relationship, or capability;
- providers able to supply it;
- the v2 projection in which it must be reachable; and
- at least one real-world reference entity plus expected provenance.

`catalog.go` embeds and validates the catalog during `go test ./...`. Executable
runners are live for movie, TV, books, music, and people through
`heya-metadata coverage verify-<domain>` and the matching `make golden-<domain>`
targets. `verify-all` / `make golden-all` runs the complete catalog. Each runner
resolves real provider IDs, reads declared projections through an in-process v2
router, and verifies stored provider evidence. Capability-only entries inspect
the real OpenAPI operation without creating discovery, resolution, or
fingerprint jobs as a side effect.

Catalog entries should be logical scopes rather than individual JSON leaves.
For example, one localized-title entry covers its value, language, country,
title type, and provenance. Add a separate entry when a fact has distinct
identity, lifecycle, provider, or API behavior.

The current live baseline is movie 37/37, TV 4/4, books 8/8, people 9/9, and
music 21/22. Music deliberately remains red for a live AcoustID-backed
fingerprint-resolution proof; that entry is an executable roadmap requirement
and must not be weakened merely to make the total green. Canonical musical
works are live through the Open Opus Symphony No. 5 canary.
