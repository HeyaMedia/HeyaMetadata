package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
)

type musicEntityInput struct {
	ID string `path:"id" format:"uuid"`
}

func registerReleases(api huma.API, runtime *platform.Runtime) {
	read := func(kind string) func(context.Context, *musicEntityInput) (*entityOutput, error) {
		return func(ctx context.Context, input *musicEntityInput) (*entityOutput, error) {
			if runtime == nil {
				return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
			}
			var body []byte
			if err := runtime.DB.QueryRow(ctx, `SELECT d.document FROM api_documents d JOIN entities e ON e.id=d.entity_id WHERE d.entity_id=$1 AND d.document_kind='detail' AND e.kind=$2`, input.ID, kind).Scan(&body); err == pgx.ErrNoRows {
				return nil, huma.Error404NotFound(kind + " not found")
			} else if err != nil {
				return nil, err
			}
			var document any
			if err := json.Unmarshal(body, &document); err != nil {
				return nil, err
			}
			return &entityOutput{Body: document}, nil
		}
	}
	huma.Register(api, huma.Operation{OperationID: "release-detail", Method: http.MethodGet, Path: "/api/v2/releases/{id}", Summary: "Get a canonical issued music release", Tags: []string{"Music"}}, read("release"))
	huma.Register(api, huma.Operation{OperationID: "recording-detail", Method: http.MethodGet, Path: "/api/v2/recordings/{id}", Summary: "Get a canonical music recording", Tags: []string{"Music"}}, read("recording"))
}
