// Package integrity audits the stored canonical graph for structural failures,
// suspicious reconciliation outcomes, and intentionally deferred work.
package integrity

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

type Sample struct {
	Reference string `json:"reference,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Label     string `json:"label,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type Check struct {
	Code        string   `json:"code"`
	Domain      string   `json:"domain"`
	Severity    Severity `json:"severity"`
	Summary     string   `json:"summary"`
	Remediation string   `json:"remediation,omitempty"`
	Count       int64    `json:"count"`
	Samples     []Sample `json:"samples,omitempty"`
}

type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	Entities    int64     `json:"entities"`
	Checks      []Check   `json:"checks"`
	Passed      int       `json:"passed"`
	Errors      int       `json:"errors"`
	Warnings    int       `json:"warnings"`
	Info        int       `json:"info"`
}

type Options struct {
	SampleLimit int
}

type checkDefinition struct {
	Check
	countSQL  string
	sampleSQL string
}

func Audit(ctx context.Context, runtime *platform.Runtime, options Options) (Report, error) {
	if runtime == nil || runtime.DB == nil {
		return Report{}, fmt.Errorf("canonical integrity audit requires PostgreSQL")
	}
	sampleLimit := options.SampleLimit
	if sampleLimit < 0 {
		sampleLimit = 0
	}
	if sampleLimit > 100 {
		sampleLimit = 100
	}
	report := Report{GeneratedAt: time.Now().UTC(), Checks: []Check{}}
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM entities WHERE deleted_at IS NULL`).Scan(&report.Entities); err != nil {
		return Report{}, fmt.Errorf("count canonical entities: %w", err)
	}
	for _, definition := range definitions() {
		check := definition.Check
		check.Samples = []Sample{}
		if err := runtime.DB.QueryRow(ctx, definition.countSQL).Scan(&check.Count); err != nil {
			return Report{}, fmt.Errorf("run integrity check %s: %w", check.Code, err)
		}
		if check.Count > 0 && sampleLimit > 0 && strings.TrimSpace(definition.sampleSQL) != "" {
			rows, err := runtime.DB.Query(ctx, definition.sampleSQL, sampleLimit)
			if err != nil {
				return Report{}, fmt.Errorf("sample integrity check %s: %w", check.Code, err)
			}
			for rows.Next() {
				var sample Sample
				if err := rows.Scan(&sample.Reference, &sample.Kind, &sample.Label, &sample.Detail); err != nil {
					rows.Close()
					return Report{}, fmt.Errorf("scan integrity check %s sample: %w", check.Code, err)
				}
				check.Samples = append(check.Samples, sample)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return Report{}, fmt.Errorf("read integrity check %s samples: %w", check.Code, err)
			}
			rows.Close()
		}
		report.Checks = append(report.Checks, check)
	}
	report.finalize()
	return report, nil
}

func (report *Report) finalize() {
	report.Passed, report.Errors, report.Warnings, report.Info = 0, 0, 0, 0
	sort.SliceStable(report.Checks, func(i, j int) bool {
		if severityRank(report.Checks[i].Severity) != severityRank(report.Checks[j].Severity) {
			return severityRank(report.Checks[i].Severity) < severityRank(report.Checks[j].Severity)
		}
		if report.Checks[i].Domain != report.Checks[j].Domain {
			return report.Checks[i].Domain < report.Checks[j].Domain
		}
		return report.Checks[i].Code < report.Checks[j].Code
	})
	for _, check := range report.Checks {
		if check.Count == 0 {
			report.Passed++
			continue
		}
		switch check.Severity {
		case SeverityError:
			report.Errors++
		case SeverityWarning:
			report.Warnings++
		case SeverityInfo:
			report.Info++
		}
	}
}

func (report Report) Error(strict bool) error {
	if report.Errors > 0 {
		return fmt.Errorf("canonical integrity audit found %d error checks", report.Errors)
	}
	if strict && report.Warnings > 0 {
		return fmt.Errorf("canonical integrity audit found %d warning checks in strict mode", report.Warnings)
	}
	return nil
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	default:
		return 2
	}
}

func definitions() []checkDefinition {
	return []checkDefinition{
		{
			Check:     Check{Code: "structural.missing_search_projection", Domain: "core", Severity: SeverityError, Summary: "live entities without a compact search projection", Remediation: "rebuild or repair the affected canonical entity projection"},
			countSQL:  `SELECT count(*) FROM entities e LEFT JOIN search_entities se ON se.entity_id=e.id WHERE e.deleted_at IS NULL AND se.entity_id IS NULL`,
			sampleSQL: `SELECT e.id::text,e.kind,e.slug,'search_entities row is absent' FROM entities e LEFT JOIN search_entities se ON se.entity_id=e.id WHERE e.deleted_at IS NULL AND se.entity_id IS NULL ORDER BY e.kind,e.slug LIMIT $1`,
		},
		{
			Check:     Check{Code: "structural.missing_detail_document", Domain: "core", Severity: SeverityError, Summary: "public canonical entities without a detail document", Remediation: "rebuild the entity; canonical people are intentionally served from canonical_people and excluded"},
			countSQL:  `SELECT count(*) FROM entities e LEFT JOIN api_documents d ON d.entity_id=e.id AND d.document_kind='detail' WHERE e.deleted_at IS NULL AND e.kind<>'person' AND d.entity_id IS NULL`,
			sampleSQL: `SELECT e.id::text,e.kind,e.slug,'detail api_document is absent' FROM entities e LEFT JOIN api_documents d ON d.entity_id=e.id AND d.document_kind='detail' WHERE e.deleted_at IS NULL AND e.kind<>'person' AND d.entity_id IS NULL ORDER BY e.kind,e.slug LIMIT $1`,
		},
		{
			Check:     Check{Code: "structural.empty_display", Domain: "core", Severity: SeverityError, Summary: "compact projections with no display title or name", Remediation: "repair normalization or the domain projection before publishing the entity"},
			countSQL:  `SELECT count(*) FROM search_entities WHERE btrim(display_title)=''`,
			sampleSQL: `SELECT entity_id::text,kind,slug,'display_title is empty' FROM search_entities WHERE btrim(display_title)='' ORDER BY kind,slug LIMIT $1`,
		},
		{
			Check:     Check{Code: "structural.providerless_entity", Domain: "core", Severity: SeverityError, Summary: "canonical entities without an accepted external identity", Remediation: "attach accepted provider identity evidence or retire the orphan entity"},
			countSQL:  `SELECT count(*) FROM entities e WHERE e.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM external_id_claims c WHERE c.entity_id=e.id AND c.state='accepted')`,
			sampleSQL: `SELECT e.id::text,e.kind,e.slug,'no accepted external_id_claims row' FROM entities e WHERE e.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM external_id_claims c WHERE c.entity_id=e.id AND c.state='accepted') ORDER BY e.kind,e.slug LIMIT $1`,
		},
		{
			Check:     Check{Code: "identity.claim_kind_mismatch", Domain: "identity", Severity: SeverityError, Summary: "external identity claims whose entity kind disagrees with the target entity", Remediation: "repair or supersede the malformed identity claim"},
			countSQL:  `SELECT count(*) FROM external_id_claims c JOIN entities e ON e.id=c.entity_id WHERE c.entity_kind<>e.kind`,
			sampleSQL: `SELECT e.id::text,e.kind,c.provider||':'||c.namespace||':'||c.normalized_value,'claim kind='||c.entity_kind FROM external_id_claims c JOIN entities e ON e.id=c.entity_id WHERE c.entity_kind<>e.kind ORDER BY c.provider,c.namespace,c.normalized_value LIMIT $1`,
		},
		{
			Check:     Check{Code: "identity.accepted_claim_collision", Domain: "identity", Severity: SeverityError, Summary: "one same-kind provider identity accepted by multiple canonical entities", Remediation: "resolve the identity conflict and merge or reject the incorrect claim"},
			countSQL:  `SELECT count(*) FROM (SELECT entity_kind,provider,namespace,normalized_value FROM external_id_claims WHERE state='accepted' GROUP BY entity_kind,provider,namespace,normalized_value HAVING count(DISTINCT entity_id)>1) collision`,
			sampleSQL: `SELECT (array_agg(entity_id ORDER BY entity_id))[1]::text,entity_kind,provider||':'||namespace||':'||normalized_value,string_agg(entity_id::text,', ' ORDER BY entity_id::text) FROM external_id_claims WHERE state='accepted' GROUP BY entity_kind,provider,namespace,normalized_value HAVING count(DISTINCT entity_id)>1 ORDER BY entity_kind,provider,namespace,normalized_value LIMIT $1`,
		},
		{
			Check:     Check{Code: "relations.source_kind_mismatch", Domain: "relations", Severity: SeverityError, Summary: "accepted relations whose source kind disagrees with the source entity", Remediation: "rebuild or remove the malformed relation"},
			countSQL:  `SELECT count(*) FROM entity_relations r JOIN entities e ON e.id=r.source_entity_id WHERE r.state='accepted' AND r.source_kind<>e.kind`,
			sampleSQL: `SELECT r.source_entity_id::text,e.kind,COALESCE(r.metadata->>'title',r.provider_value),'relation source_kind='||r.source_kind FROM entity_relations r JOIN entities e ON e.id=r.source_entity_id WHERE r.state='accepted' AND r.source_kind<>e.kind ORDER BY r.last_observed_at DESC LIMIT $1`,
		},
		{
			Check:     Check{Code: "relations.target_kind_mismatch", Domain: "relations", Severity: SeverityError, Summary: "accepted relations whose target kind disagrees with the canonical target", Remediation: "reconcile the target identity and rebuild the relation"},
			countSQL:  `SELECT count(*) FROM entity_relations r JOIN entities e ON e.id=r.target_entity_id WHERE r.state='accepted' AND r.target_kind<>e.kind`,
			sampleSQL: `SELECT r.target_entity_id::text,e.kind,COALESCE(r.metadata->>'title',r.provider_value),'relation target_kind='||r.target_kind FROM entity_relations r JOIN entities e ON e.id=r.target_entity_id WHERE r.state='accepted' AND r.target_kind<>e.kind ORDER BY r.last_observed_at DESC LIMIT $1`,
		},
		{
			Check:     Check{Code: "relations.target_missing_search_projection", Domain: "relations", Severity: SeverityError, Summary: "relations pointing at canonical targets that cannot be read as compact entities", Remediation: "rebuild the target projection or clear the invalid target"},
			countSQL:  `SELECT count(*) FROM entity_relations r LEFT JOIN search_entities se ON se.entity_id=r.target_entity_id WHERE r.state='accepted' AND r.target_entity_id IS NOT NULL AND se.entity_id IS NULL`,
			sampleSQL: `SELECT r.target_entity_id::text,r.target_kind,COALESCE(r.metadata->>'title',r.provider_value),'source='||r.source_entity_id::text||' relation='||r.relation_type FROM entity_relations r LEFT JOIN search_entities se ON se.entity_id=r.target_entity_id WHERE r.state='accepted' AND r.target_entity_id IS NOT NULL AND se.entity_id IS NULL ORDER BY r.last_observed_at DESC LIMIT $1`,
		},
		{
			Check:     Check{Code: "projection.provenance_version_mismatch", Domain: "projections", Severity: SeverityError, Summary: "detail provenance whose projection version differs from its API document", Remediation: "atomically rebuild the detail document and provenance projection"},
			countSQL:  `SELECT count(*) FROM api_document_provenance p JOIN api_documents d USING(entity_id,document_kind) WHERE p.projection_version<>d.projection_version`,
			sampleSQL: `SELECT p.entity_id::text,e.kind,se.display_title,'document='||d.projection_version||' provenance='||p.projection_version FROM api_document_provenance p JOIN api_documents d USING(entity_id,document_kind) JOIN entities e ON e.id=p.entity_id LEFT JOIN search_entities se ON se.entity_id=p.entity_id WHERE p.projection_version<>d.projection_version ORDER BY e.kind,se.display_title LIMIT $1`,
		},
		{
			Check:     Check{Code: "projection.version_ahead_of_entity", Domain: "projections", Severity: SeverityError, Summary: "published projections newer than the owning entity canonical version", Remediation: "make every projection increment entities.canonical_version atomically"},
			countSQL:  `SELECT count(*) FROM entities e WHERE e.deleted_at IS NULL AND EXISTS(SELECT 1 FROM api_documents d WHERE d.entity_id=e.id AND d.projection_version>e.canonical_version)`,
			sampleSQL: `SELECT e.id::text,e.kind,COALESCE(se.display_title,e.slug),'entity='||e.canonical_version||' projection='||max(d.projection_version) FROM entities e JOIN api_documents d ON d.entity_id=e.id LEFT JOIN search_entities se ON se.entity_id=e.id WHERE e.deleted_at IS NULL AND d.projection_version>e.canonical_version GROUP BY e.id,e.kind,se.display_title,e.slug,e.canonical_version ORDER BY e.kind,COALESCE(se.display_title,e.slug) LIMIT $1`,
		},
		{
			Check:     Check{Code: "identity.open_conflicts", Domain: "identity", Severity: SeverityWarning, Summary: "unresolved external identity conflicts", Remediation: "review, resolve, or dismiss the conflict with durable evidence"},
			countSQL:  `SELECT count(*) FROM external_id_conflicts WHERE state='open'`,
			sampleSQL: `SELECT id::text,entity_kind,id::text,'created='||created_at::text FROM external_id_conflicts WHERE state='open' ORDER BY created_at LIMIT $1`,
		},
		{
			Check:     Check{Code: "freshness.stale_detail", Domain: "freshness", Severity: SeverityWarning, Summary: "canonical detail documents past their freshness deadline", Remediation: "enqueue a background refresh according to access-weighted priority"},
			countSQL:  `SELECT count(*) FROM api_documents WHERE document_kind='detail' AND fresh_until<=now()`,
			sampleSQL: `SELECT d.entity_id::text,e.kind,COALESCE(se.display_title,e.slug),'fresh_until='||d.fresh_until::text FROM api_documents d JOIN entities e ON e.id=d.entity_id LEFT JOIN search_entities se ON se.entity_id=d.entity_id WHERE d.document_kind='detail' AND d.fresh_until<=now() ORDER BY d.fresh_until LIMIT $1`,
		},
		{
			Check:     Check{Code: "images.missing_primary_artwork", Domain: "images", Severity: SeverityWarning, Summary: "display-oriented root entities with no artwork candidate", Remediation: "refresh image-capable providers or record the intentional absence"},
			countSQL:  `SELECT count(*) FROM search_entities se WHERE se.kind=ANY(ARRAY['movie','tv_show','anime','artist','book_work','manga','manga_volume']) AND NOT EXISTS(SELECT 1 FROM image_candidates i WHERE i.entity_id=se.entity_id)`,
			sampleSQL: `SELECT se.entity_id::text,se.kind,se.display_title,'no image_candidates row' FROM search_entities se WHERE se.kind=ANY(ARRAY['movie','tv_show','anime','artist','book_work','manga','manga_volume']) AND NOT EXISTS(SELECT 1 FROM image_candidates i WHERE i.entity_id=se.entity_id) ORDER BY se.kind,se.display_title LIMIT $1`,
		},
		{
			Check:     Check{Code: "images.failed_materialization", Domain: "images", Severity: SeverityWarning, Summary: "image candidates in a failed materialization state", Remediation: "retry transient failures and retire permanently invalid source URLs"},
			countSQL:  `SELECT count(*) FROM image_candidates WHERE materialization_state=ANY(ARRAY['failed','error'])`,
			sampleSQL: `SELECT i.entity_id::text,e.kind,COALESCE(se.display_title,e.slug),i.provider||':'||i.class||' state='||i.materialization_state FROM image_candidates i JOIN entities e ON e.id=i.entity_id LEFT JOIN search_entities se ON se.entity_id=i.entity_id WHERE i.materialization_state=ANY(ARRAY['failed','error']) ORDER BY i.created_at DESC LIMIT $1`,
		},
		{
			Check:     Check{Code: "providers.latest_partial_normalization", Domain: "providers", Severity: SeverityWarning, Summary: "latest provider records marked as partially normalized", Remediation: "inspect normalizer warnings before treating the provider contribution as complete"},
			countSQL:  `SELECT count(*) FROM (SELECT DISTINCT ON(entity_kind,entity_id,provider) partial_failure FROM normalized_records WHERE entity_id IS NOT NULL ORDER BY entity_kind,entity_id,provider,observed_at DESC) latest WHERE partial_failure`,
			sampleSQL: `WITH latest AS (SELECT DISTINCT ON(n.entity_kind,n.entity_id,n.provider) n.* FROM normalized_records n WHERE n.entity_id IS NOT NULL ORDER BY n.entity_kind,n.entity_id,n.provider,n.observed_at DESC) SELECT latest.entity_id::text,latest.entity_kind,COALESCE(se.display_title,latest.provider_record_id),latest.provider||' warnings='||jsonb_array_length(latest.warnings) FROM latest LEFT JOIN search_entities se ON se.entity_id=latest.entity_id WHERE latest.partial_failure ORDER BY latest.observed_at DESC LIMIT $1`,
		},
		{
			Check:     Check{Code: "music.unresolved_discography", Domain: "music", Severity: SeverityWarning, Summary: "accepted discography clusters without a canonical release-group target", Remediation: "strengthen cross-provider evidence, reject noise, or promote a canonical target"},
			countSQL:  `SELECT count(*) FROM entity_relations WHERE state='accepted' AND relation_type='discography' AND target_entity_id IS NULL`,
			sampleSQL: `SELECT r.source_entity_id::text,r.source_kind,COALESCE(r.metadata->>'title',r.provider_value),r.provider||':'||r.namespace||':'||r.provider_value||' state='||COALESCE(r.metadata->>'resolution_state','') FROM entity_relations r WHERE r.state='accepted' AND r.relation_type='discography' AND r.target_entity_id IS NULL ORDER BY NULLIF(r.metadata->>'first_release_date','') DESC NULLS LAST LIMIT $1`,
		},
		{
			Check:     Check{Code: "music.possible_duplicate_release_groups", Domain: "music", Severity: SeverityWarning, Summary: "release groups sharing normalized title, year, and artist credit", Remediation: "compare provider identities and track evidence; merge only when the release-group identity is truly equivalent"},
			countSQL:  `SELECT count(*) FROM (SELECT lower(unaccent(display_title)),release_year,lower(COALESCE(summary->'display'->>'artist_credit','')) FROM search_entities WHERE kind='release_group' GROUP BY 1,2,3 HAVING count(*)>1) duplicate`,
			sampleSQL: `SELECT (array_agg(entity_id ORDER BY entity_id))[1]::text,'release_group',min(display_title),COALESCE(summary->'display'->>'artist_credit','')||' · '||COALESCE(release_year::text,'unknown year')||' · '||string_agg(entity_id::text,', ' ORDER BY entity_id::text) FROM search_entities WHERE kind='release_group' GROUP BY lower(unaccent(display_title)),release_year,lower(COALESCE(summary->'display'->>'artist_credit','')),summary->'display'->>'artist_credit' HAVING count(*)>1 ORDER BY count(*) DESC,min(display_title) LIMIT $1`,
		},
		{
			Check:     Check{Code: "music.deferred_releases", Domain: "music", Severity: SeverityInfo, Summary: "provider-backed issued releases intentionally not materialized as canonical release entities", Remediation: "monitor the backlog; materialize releases on demand or when track evidence is needed"},
			countSQL:  `SELECT count(*) FROM entity_relations WHERE state='accepted' AND relation_type='editions' AND source_kind='release_group' AND target_kind='release' AND target_entity_id IS NULL`,
			sampleSQL: `SELECT r.source_entity_id::text,r.source_kind,COALESCE(r.metadata->>'title',r.provider_value),r.provider||':'||r.namespace||':'||r.provider_value FROM entity_relations r WHERE r.state='accepted' AND r.relation_type='editions' AND r.source_kind='release_group' AND r.target_kind='release' AND r.target_entity_id IS NULL ORDER BY r.last_observed_at DESC LIMIT $1`,
		},
		{
			Check:     Check{Code: "identity.cross_kind_claim_reuse", Domain: "identity", Severity: SeverityInfo, Summary: "provider identifiers reused across canonical abstraction levels", Remediation: "usually valid for release versus release-group projections; inspect unexpected kind pairs"},
			countSQL:  `SELECT count(*) FROM (SELECT provider,namespace,normalized_value FROM external_id_claims WHERE state='accepted' GROUP BY provider,namespace,normalized_value HAVING count(DISTINCT entity_kind)>1) reused`,
			sampleSQL: `SELECT (array_agg(entity_id ORDER BY entity_id))[1]::text,string_agg(DISTINCT entity_kind,',' ORDER BY entity_kind),provider||':'||namespace||':'||normalized_value,string_agg(entity_id::text,', ' ORDER BY entity_id::text) FROM external_id_claims WHERE state='accepted' GROUP BY provider,namespace,normalized_value HAVING count(DISTINCT entity_kind)>1 ORDER BY provider,namespace,normalized_value LIMIT $1`,
		},
	}
}
