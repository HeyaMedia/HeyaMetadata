# Semantic coverage catalog

This directory records product capabilities that must survive the clean-slate
rewrite. It does not describe the old API shape.

Each entry states:

- the logical fact, relationship, or capability;
- providers able to supply it;
- the v2 projection in which it must be reachable; and
- at least one real-world reference entity plus expected provenance.

`catalog.go` embeds and validates the catalog during `go test ./...`. Later
integration tests will use the same entries to resolve the reference entities,
read the declared v2 projection, and verify the expected provider provenance.

Catalog entries should be logical scopes rather than individual JSON leaves.
For example, one localized-title entry covers its value, language, country,
title type, and provenance. Add a separate entry when a fact has distinct
identity, lifecycle, provider, or API behavior.

The movie and music catalogs are the first domain inventories. Music includes
explicit boundary capabilities for release groups, releases, recordings,
release tracks, and musical works before those projections are implemented.
