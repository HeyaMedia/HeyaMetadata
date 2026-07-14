// Package canonicalrefs resolves passive provider evidence to canonical Heya
// entities without exposing provider routing to API clients.
package canonicalrefs

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

type Ref struct {
	Provider  string
	Namespace string
	Value     string
}

type Querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func Key(ref Ref) string {
	return strings.ToLower(strings.TrimSpace(ref.Provider)) + "\x00" +
		strings.ToLower(strings.TrimSpace(ref.Namespace)) + "\x00" +
		strings.ToLower(strings.TrimSpace(ref.Value))
}

// Resolve returns only accepted claims attached to active canonical entities.
func Resolve(ctx context.Context, db Querier, entityKind string, refs []Ref) (map[string]string, error) {
	resolved := map[string]string{}
	providers, namespaces, values := []string{}, []string{}, []string{}
	seenProviders, seenNamespaces, seenValues := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, ref := range refs {
		provider := strings.ToLower(strings.TrimSpace(ref.Provider))
		namespace := strings.ToLower(strings.TrimSpace(ref.Namespace))
		value := strings.ToLower(strings.TrimSpace(ref.Value))
		if provider == "heya" && namespace == entityKind && value != "" {
			resolved[Key(ref)] = value
			continue
		}
		if provider == "" || namespace == "" || value == "" {
			continue
		}
		if !seenProviders[provider] {
			seenProviders[provider] = true
			providers = append(providers, provider)
		}
		if !seenNamespaces[namespace] {
			seenNamespaces[namespace] = true
			namespaces = append(namespaces, namespace)
		}
		if !seenValues[value] {
			seenValues[value] = true
			values = append(values, value)
		}
	}
	if len(providers) == 0 {
		return resolved, nil
	}
	rows, err := db.Query(ctx, `SELECT claim.provider,claim.namespace,claim.normalized_value,claim.entity_id::text FROM external_id_claims claim JOIN entities entity ON entity.id=claim.entity_id AND entity.kind=$1 AND entity.deleted_at IS NULL WHERE claim.entity_kind=$1 AND claim.state='accepted' AND claim.provider=ANY($2) AND claim.namespace=ANY($3) AND lower(claim.normalized_value)=ANY($4)`, entityKind, providers, namespaces, values)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref Ref
		var entityID string
		if err := rows.Scan(&ref.Provider, &ref.Namespace, &ref.Value, &entityID); err != nil {
			return nil, err
		}
		resolved[Key(ref)] = entityID
	}
	return resolved, rows.Err()
}
