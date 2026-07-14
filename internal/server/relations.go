package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/danielgtaylor/huma/v2"
)

type entityRelationsInput struct {
	ID     string `path:"id" minLength:"1" maxLength:"100"`
	Type   string `query:"type" maxLength:"100" doc:"Optional exact relation type, such as discography"`
	Offset int    `query:"offset" minimum:"0" default:"0"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
}

type entityRelation struct {
	ID              string          `json:"id" format:"uuid"`
	RelationType    string          `json:"relation_type"`
	SourceKind      string          `json:"source_kind"`
	TargetKind      string          `json:"target_kind"`
	TargetEntityID  string          `json:"target_entity_id,omitempty" format:"uuid"`
	Provider        string          `json:"provider"`
	Namespace       string          `json:"namespace"`
	ProviderValue   string          `json:"provider_value"`
	Position        *int            `json:"position,omitempty"`
	Metadata        json.RawMessage `json:"metadata"`
	Target          json.RawMessage `json:"target,omitempty"`
	LastObservedAt  time.Time       `json:"last_observed_at"`
	ResolutionState string          `json:"resolution_state" enum:"materialized,unresolved"`
}

type entityRelationsOutput struct {
	Body struct {
		Relations []entityRelation `json:"relations"`
		Total     int64            `json:"total"`
		Offset    int              `json:"offset"`
		Limit     int              `json:"limit"`
	}
}

func registerRelations(api huma.API, runtime *platform.Runtime) {
	huma.Register(api, huma.Operation{
		OperationID: "entity-relations",
		Method:      http.MethodGet,
		Path:        "/api/v2/entities/{id}/relations",
		Summary:     "Canonical and provider-backed entity relationships",
		Tags:        []string{"Entities", "Relations"},
	}, func(ctx context.Context, input *entityRelationsInput) (*entityRelationsOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		return listEntityRelations(ctx, runtime, input)
	})
}

func listEntityRelations(ctx context.Context, runtime *platform.Runtime, input *entityRelationsInput) (*entityRelationsOutput, error) {
	limit := input.Limit
	if limit < 1 || limit > 100 {
		limit = 50
	}
	offset := input.Offset
	if offset < 0 {
		offset = 0
	}
	relationType := strings.ToLower(strings.TrimSpace(input.Type))

	var exists bool
	if err := runtime.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM entities WHERE id=$1 AND deleted_at IS NULL)`, input.ID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, huma.Error404NotFound("entity not found")
	}

	out := &entityRelationsOutput{}
	out.Body.Relations = []entityRelation{}
	out.Body.Offset = offset
	out.Body.Limit = limit
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM entity_relations WHERE source_entity_id=$1 AND state='accepted' AND ($2='' OR relation_type=$2)`, input.ID, relationType).Scan(&out.Body.Total); err != nil {
		return nil, err
	}
	orderBy := `r.position NULLS LAST,
		         NULLIF(r.metadata->>'first_release_date','') NULLS LAST,
		         lower(COALESCE(r.metadata->>'title',r.provider_value)),r.id`
	if relationType == "discography" {
		orderBy = `NULLIF(r.metadata->>'first_release_date','') DESC NULLS LAST,
		           lower(COALESCE(r.metadata->>'title',r.provider_value)),r.id`
	}
	rows, err := runtime.DB.Query(ctx, `
		SELECT r.id::text,r.relation_type,r.source_kind,r.target_kind,
		       COALESCE(r.target_entity_id::text,''),r.provider,r.namespace,
		       r.provider_value,r.position,r.metadata,COALESCE(se.summary,'null'::jsonb),
		       r.last_observed_at
		FROM entity_relations r
		LEFT JOIN search_entities se ON se.entity_id=r.target_entity_id
		WHERE r.source_entity_id=$1 AND r.state='accepted'
		  AND ($2='' OR r.relation_type=$2)
		ORDER BY `+orderBy+`
		OFFSET $3 LIMIT $4`, input.ID, relationType, offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var relation entityRelation
		var metadata, target []byte
		if err := rows.Scan(
			&relation.ID, &relation.RelationType, &relation.SourceKind, &relation.TargetKind,
			&relation.TargetEntityID, &relation.Provider, &relation.Namespace,
			&relation.ProviderValue, &relation.Position, &metadata, &target,
			&relation.LastObservedAt,
		); err != nil {
			return nil, err
		}
		relation.Metadata = json.RawMessage(metadata)
		if string(target) != "null" {
			relation.Target = json.RawMessage(target)
		}
		relation.ResolutionState = "unresolved"
		if relation.TargetEntityID != "" {
			relation.ResolutionState = "materialized"
		}
		out.Body.Relations = append(out.Body.Relations, relation)
	}
	return out, rows.Err()
}
