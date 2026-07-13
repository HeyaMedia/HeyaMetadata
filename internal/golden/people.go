package golden

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"

	semanticcoverage "github.com/HeyaMedia/HeyaMetadata/coverage"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/server"
)

type Check struct {
	EntryID   string `json:"entry_id"`
	Reference string `json:"reference"`
	EntityID  string `json:"entity_id,omitempty"`
	Passed    bool   `json:"passed"`
	Error     string `json:"error,omitempty"`
}

type Report struct {
	Domain string  `json:"domain"`
	Passed int     `json:"passed"`
	Failed int     `json:"failed"`
	Checks []Check `json:"checks"`
}

func (r Report) Error() error {
	if r.Failed == 0 {
		return nil
	}
	return fmt.Errorf("%s golden coverage failed %d of %d checks", r.Domain, r.Failed, len(r.Checks))
}

func VerifyPeople(ctx context.Context, runtime *platform.Runtime) (Report, error) {
	catalog, err := semanticcoverage.People()
	if err != nil {
		return Report{}, err
	}
	testServer := httptest.NewServer(server.NewWithRuntime("golden-coverage", runtime).Handler())
	defer testServer.Close()
	report := Report{Domain: "people", Checks: []Check{}}
	for _, entry := range catalog.Entries {
		for _, reference := range entry.References {
			check := Check{EntryID: entry.ID, Reference: reference.Name}
			entityID, resolveErr := resolveReference(ctx, runtime, entry.Kind, reference.ExternalIDs)
			if resolveErr != nil {
				check.Error = resolveErr.Error()
				report.Checks = append(report.Checks, check)
				continue
			}
			check.EntityID = entityID
			if entry.Projection.Document == "internal" {
				check.Passed, check.Error = verifyInternalReconciliation(ctx, runtime, entityID)
			} else {
				check.Passed, check.Error = verifyPersonProjection(ctx, testServer.Client(), testServer.URL, entityID, entry.Projection.Path)
			}
			if check.Passed {
				for _, provider := range reference.ExpectedProviders {
					if provider == "system" {
						continue
					}
					var present bool
					if err := runtime.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM external_id_claims WHERE entity_id=$1 AND entity_kind='person' AND provider=$2 AND state='accepted') OR EXISTS(SELECT 1 FROM normalized_records WHERE entity_id=$1 AND entity_kind='person' AND provider=$2) OR EXISTS(SELECT 1 FROM person_provider_credits WHERE person_entity_id=$1 AND provider=$2) OR EXISTS(SELECT 1 FROM image_candidates WHERE entity_id=$1 AND provider=$2)`, entityID, provider).Scan(&present); err != nil {
						check.Passed, check.Error = false, err.Error()
						break
					}
					if !present {
						check.Passed, check.Error = false, fmt.Sprintf("expected %s provenance is absent", provider)
						break
					}
				}
			}
			report.Checks = append(report.Checks, check)
		}
	}
	for index := range report.Checks {
		if report.Checks[index].Passed {
			report.Passed++
		} else {
			report.Failed++
		}
	}
	return report, nil
}

func resolveReference(ctx context.Context, runtime *platform.Runtime, kind string, externalIDs map[string]string) (string, error) {
	providers := make([]string, 0, len(externalIDs))
	for provider := range externalIDs {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	resolved := ""
	for _, provider := range providers {
		var entityID string
		err := runtime.DB.QueryRow(ctx, `SELECT claim.entity_id::text FROM external_id_claims claim JOIN entities entity ON entity.id=claim.entity_id AND entity.deleted_at IS NULL WHERE claim.entity_kind=$1 AND claim.provider=$2 AND claim.normalized_value=$3 AND claim.state='accepted' LIMIT 1`, kind, provider, externalIDs[provider]).Scan(&entityID)
		if err != nil {
			return "", fmt.Errorf("%s:%s is not resolvable as %s", provider, externalIDs[provider], kind)
		}
		if resolved != "" && resolved != entityID {
			return "", fmt.Errorf("reference external IDs resolve to both %s and %s", resolved, entityID)
		}
		resolved = entityID
	}
	if resolved == "" {
		return "", fmt.Errorf("reference has no external IDs")
	}
	return resolved, nil
}

func verifyPersonProjection(ctx context.Context, client *http.Client, baseURL, entityID, path string) (bool, string) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v2/persons/"+entityID, nil)
	if err != nil {
		return false, err.Error()
	}
	response, err := client.Do(request)
	if err != nil {
		return false, err.Error()
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("person detail returned HTTP %d", response.StatusCode)
	}
	var document any
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		return false, err.Error()
	}
	value, ok := valueAtPath(document, path)
	if !ok || emptyValue(value) {
		return false, fmt.Sprintf("projection path %s is empty", path)
	}
	return true, ""
}

func verifyInternalReconciliation(ctx context.Context, runtime *platform.Runtime, entityID string) (bool, string) {
	var accepted int
	err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM person_reconciliation_candidates candidate JOIN moderation_audit_log audit ON audit.id=candidate.audit_log_id WHERE candidate.state='accepted' AND candidate.survivor_person_id=$1 AND audit.action='person_reconciliation_accept'`, entityID).Scan(&accepted)
	if err != nil {
		return false, err.Error()
	}
	if accepted == 0 {
		return false, "no audited accepted reconciliation targets this canonical person"
	}
	return true, ""
}

func valueAtPath(document any, path string) (any, bool) {
	values := valuesAtPath(document, strings.Split(path, "."))
	if len(values) == 0 {
		return nil, false
	}
	if len(values) == 1 {
		return values[0], true
	}
	return values, true
}

func valuesAtPath(current any, parts []string) []any {
	if len(parts) == 0 {
		return []any{current}
	}
	part := parts[0]
	if strings.HasSuffix(part, "[]") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		items, ok := object[strings.TrimSuffix(part, "[]")].([]any)
		if !ok {
			return nil
		}
		var values []any
		for _, item := range items {
			values = append(values, valuesAtPath(item, parts[1:])...)
		}
		return values
	}
	object, ok := current.(map[string]any)
	if !ok {
		return nil
	}
	next, ok := object[part]
	if !ok {
		return nil
	}
	return valuesAtPath(next, parts[1:])
}

func emptyValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		if len(typed) == 0 {
			return true
		}
		for _, item := range typed {
			if !emptyValue(item) {
				return false
			}
		}
		return true
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}
