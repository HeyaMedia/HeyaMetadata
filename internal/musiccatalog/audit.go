package musiccatalog

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
)

type AuditSummary struct {
	ArtistID         string               `json:"artist_id"`
	ArtistName       string               `json:"artist_name"`
	Clusters         int                  `json:"clusters"`
	CanonicalTargets int                  `json:"canonical_targets"`
	ProviderOnly     int                  `json:"provider_only"`
	CandidateOnly    int                  `json:"candidate_only"`
	Unresolved       int                  `json:"unresolved"`
	Conflicts        int                  `json:"conflicts"`
	PromotionFailed  int                  `json:"promotion_failed"`
	Weak             int                  `json:"weak"`
	StrongBridges    int                  `json:"strong_evidence_bridges"`
	Providers        map[string]int       `json:"providers"`
	PotentialDupes   []PotentialDuplicate `json:"potential_duplicates"`
}

type PotentialDuplicate struct {
	LeftID     string `json:"left_id"`
	LeftTitle  string `json:"left_title"`
	RightID    string `json:"right_id"`
	RightTitle string `json:"right_title"`
	Reason     string `json:"reason"`
}

type auditRelation struct {
	id, title, date, kind, state string
	confidence                   float64
	target                       bool
	providers                    []string
}

func AuditArtist(ctx context.Context, runtime *platform.Runtime, artistID string) (AuditSummary, error) {
	report := AuditSummary{ArtistID: artistID, Providers: map[string]int{}, PotentialDupes: []PotentialDuplicate{}}
	if err := runtime.DB.QueryRow(ctx, `SELECT display_title FROM search_entities WHERE entity_id=$1 AND kind='artist'`, artistID).Scan(&report.ArtistName); err != nil {
		return report, err
	}
	_ = runtime.DB.QueryRow(ctx, `SELECT count(*) FROM entity_relations WHERE source_entity_id=$1 AND relation_type='discography_candidate' AND state='accepted'`, artistID).Scan(&report.CandidateOnly)
	rows, err := runtime.DB.Query(ctx, `SELECT id::text,target_entity_id IS NOT NULL,metadata FROM entity_relations WHERE source_entity_id=$1 AND relation_type='discography' AND state='accepted' ORDER BY metadata->>'title'`, artistID)
	if err != nil {
		return report, err
	}
	defer rows.Close()
	values := []auditRelation{}
	for rows.Next() {
		var id string
		var target bool
		var raw []byte
		if err := rows.Scan(&id, &target, &raw); err != nil {
			return report, err
		}
		var metadata struct {
			Title        string  `json:"title"`
			Date         string  `json:"first_release_date"`
			Kind         string  `json:"primary_type"`
			Confidence   float64 `json:"confidence"`
			State        string  `json:"resolution_state"`
			BridgeReason string  `json:"bridge_reason"`
			Sources      []struct {
				Provider string `json:"provider"`
			} `json:"sources"`
		}
		if err := json.Unmarshal(raw, &metadata); err != nil {
			return report, err
		}
		value := auditRelation{id: id, title: metadata.Title, date: metadata.Date, kind: metadata.Kind, state: metadata.State, confidence: metadata.Confidence, target: target}
		for _, source := range metadata.Sources {
			value.providers = append(value.providers, source.Provider)
			report.Providers[source.Provider]++
		}
		values = append(values, value)
		report.Clusters++
		if target {
			report.CanonicalTargets++
		} else {
			report.ProviderOnly++
		}
		switch metadata.State {
		case "unresolved_single_provider":
			report.Unresolved++
		case "identity_conflict":
			report.Conflicts++
		case "promotion_failed":
			report.PromotionFailed++
		}
		if metadata.Confidence < .8 {
			report.Weak++
		}
		if metadata.BridgeReason != "" {
			report.StrongBridges++
		}
	}
	if err := rows.Err(); err != nil {
		return report, err
	}
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			a, b := candidate{Provider: "audit-a", Title: values[i].title, Date: values[i].date, Kind: values[i].kind}, candidate{Provider: "audit-b", Title: values[j].title, Date: values[j].date, Kind: values[j].kind}
			reason := ""
			if ok, why, _ := candidateMatch(a, b); ok {
				reason = why
			} else if exactDateCrossScript(a, b) {
				reason = "unique_exact_date_type_cross_script"
			}
			if reason != "" {
				report.PotentialDupes = append(report.PotentialDupes, PotentialDuplicate{LeftID: values[i].id, LeftTitle: values[i].title, RightID: values[j].id, RightTitle: values[j].title, Reason: reason})
			}
		}
	}
	sort.Slice(report.PotentialDupes, func(i, j int) bool { return report.PotentialDupes[i].LeftTitle < report.PotentialDupes[j].LeftTitle })
	if len(report.PotentialDupes) > 100 {
		report.PotentialDupes = report.PotentialDupes[:100]
	}
	return report, nil
}
