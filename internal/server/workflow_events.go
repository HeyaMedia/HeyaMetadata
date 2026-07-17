package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/workflowfeed"
	"github.com/danielgtaylor/huma/v2"
)

type workflowEventsInput struct {
	After    int64  `query:"after" minimum:"0"`
	Limit    int    `query:"limit" minimum:"1" maximum:"500" default:"100"`
	StreamID string `query:"stream_id" format:"uuid" doc:"Previously observed stream identity; a mismatch returns workflow_stream_changed"`
}
type workflowEventsOutput struct {
	ServerTiming string `header:"Server-Timing"`
	Body         struct {
		StreamID   string               `json:"stream_id" format:"uuid"`
		HeadCursor int64                `json:"head_cursor" minimum:"0"`
		Events     []workflowfeed.Event `json:"events" nullable:"false"`
		NextCursor int64                `json:"next_cursor" minimum:"0"`
	}
}

func registerWorkflowEvents(api huma.API, runtime *platform.Runtime) {
	huma.Register(api, huma.Operation{
		OperationID: "workflow-events",
		Method:      http.MethodGet,
		Path:        "/api/v2/workflow-events",
		Summary:     "Read the gap-free workflow completion feed",
		Description: "Announces async workflows (discovery runs) reaching completed or failed, including completions that never touch a canonical entity and therefore never appear in /api/v2/changes. Consumers waiting on parked workflows should poll this feed with their cursor instead of polling each workflow, and must reset to cursor zero and replay idempotently after workflow_stream_changed or workflow_cursor_ahead.",
		Tags:        []string{"Changes"},
		Responses: map[string]*huma.Response{
			"409":     withServerTiming(problemJSONResponse("The supplied stream identity changed or the cursor is ahead of the available stream", "#/components/schemas/ErrorModel")),
			"default": problemJSONResponse("Error", "#/components/schemas/ErrorModel"),
		},
	}, func(ctx context.Context, input *workflowEventsInput) (*workflowEventsOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		started := time.Now()
		page, err := workflowfeed.ReadPage(ctx, runtime, input.StreamID, input.After, input.Limit)
		if err != nil {
			var conflict *workflowfeed.CursorConflict
			if errors.As(err, &conflict) {
				duration := time.Since(started)
				slog.InfoContext(ctx, "workflow event cursor rejected", "code", conflict.Code, "stream_id", conflict.StreamID, "head_cursor", conflict.Head, "after", input.After, "duration_ms", duration.Milliseconds())
				problem := changeFeedConflict(conflict.Code, conflict.Error(), conflict.StreamID, conflict.Head)
				return nil, huma.ErrorWithHeaders(problem, http.Header{"Server-Timing": {serverTiming("workflow-events", duration)}})
			}
			return nil, err
		}
		output := &workflowEventsOutput{ServerTiming: serverTiming("workflow-events", page.QueryTime)}
		output.Body.StreamID = page.StreamID
		output.Body.HeadCursor = page.Head
		output.Body.Events = page.Events
		output.Body.NextCursor = page.Next
		slog.InfoContext(ctx, "workflow event feed read", "stream_id", page.StreamID, "head_cursor", page.Head, "after", input.After, "next_cursor", page.Next, "event_count", len(page.Events), "duration_ms", page.QueryTime.Milliseconds())
		return output, nil
	})
}
