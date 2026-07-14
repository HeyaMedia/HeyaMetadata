package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/auth"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/danielgtaylor/huma/v2"
)

// Admin-only introspection. Every operation is gated on the caller's canonical
// role, resolved from the browser session or an API key — the same identity
// path as the rest of auth.

type adminJobsInput struct {
	Session       string `cookie:"__Host-heya_session" doc:"Opaque browser session"`
	Authorization string `header:"Authorization" doc:"Optional Heya API key using the Bearer scheme"`
	State         string `query:"state" doc:"Filter by River state (available, running, retryable, scheduled, completed, cancelled, discarded); empty for all"`
	Kind          string `query:"kind" doc:"Filter by job kind"`
	Limit         int    `query:"limit" default:"50" minimum:"1" maximum:"200"`
}

type jobStateCount struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

type adminJob struct {
	ID          int64           `json:"id"`
	Kind        string          `json:"kind"`
	State       string          `json:"state"`
	Queue       string          `json:"queue"`
	Attempt     int             `json:"attempt"`
	MaxAttempts int             `json:"max_attempts"`
	Priority    int             `json:"priority"`
	CreatedAt   time.Time       `json:"created_at"`
	ScheduledAt time.Time       `json:"scheduled_at"`
	AttemptedAt *time.Time      `json:"attempted_at,omitempty"`
	FinalizedAt *time.Time      `json:"finalized_at,omitempty"`
	Error       string          `json:"error,omitempty"`
	Args        json.RawMessage `json:"args,omitempty"`
}

type adminJobsBody struct {
	Summary []jobStateCount `json:"summary"`
	Jobs    []adminJob      `json:"jobs"`
	Total   int             `json:"total"`
}

type adminJobsOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         adminJobsBody
}

type adminJobActionInput struct {
	Session       string `cookie:"__Host-heya_session"`
	Authorization string `header:"Authorization"`
	Body          struct {
		Action string `json:"action" enum:"clear_completed,clear_queue,rescue_stuck" doc:"clear_completed removes finished history; clear_queue drops waiting jobs; rescue_stuck requeues jobs stuck running"`
	}
}

type adminJobActionOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         struct {
		Action   string `json:"action"`
		Affected int64  `json:"affected"`
	}
}

// stuckRunningInterval defines how long a job must have been running before
// rescue_stuck treats it as abandoned (worker crash) and requeues it.
const stuckRunningInterval = "15 minutes"

func registerAdmin(api huma.API, runtime *platform.Runtime) {
	var service *auth.Service
	if runtime != nil {
		service = auth.New(runtime.DB, runtime.Redis)
	}

	huma.Register(api, huma.Operation{
		OperationID: "admin-jobs",
		Method:      http.MethodGet,
		Path:        "/api/v2/admin/jobs",
		Summary:     "Inspect the River job queue",
		Description: "Returns queue state counts and a recent slice of jobs. Admin role required.",
		Tags:        []string{"Admin"},
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable},
	}, func(ctx context.Context, input *adminJobsInput) (*adminJobsOutput, error) {
		if service == nil || runtime == nil {
			return nil, huma.Error503ServiceUnavailable("admin service is unavailable")
		}
		if _, err := requireAdmin(ctx, service, input.Session, input.Authorization); err != nil {
			return nil, err
		}

		body := adminJobsBody{Summary: []jobStateCount{}, Jobs: []adminJob{}}

		summaryRows, err := runtime.DB.Query(ctx, `SELECT state::text, count(*) FROM river_job GROUP BY state ORDER BY count(*) DESC`)
		if err != nil {
			return nil, huma.Error503ServiceUnavailable("job queue is unavailable")
		}
		for summaryRows.Next() {
			var row jobStateCount
			if err := summaryRows.Scan(&row.State, &row.Count); err != nil {
				summaryRows.Close()
				return nil, huma.Error503ServiceUnavailable("job queue is unavailable")
			}
			body.Summary = append(body.Summary, row)
			body.Total += row.Count
		}
		summaryRows.Close()
		if err := summaryRows.Err(); err != nil {
			return nil, huma.Error503ServiceUnavailable("job queue is unavailable")
		}

		jobRows, err := runtime.DB.Query(ctx, `
			SELECT id, kind, state::text, queue, attempt, max_attempts, priority,
			       created_at, scheduled_at, attempted_at, finalized_at, args,
			       CASE WHEN errors IS NOT NULL AND array_length(errors, 1) > 0
			            THEN errors[array_length(errors, 1)] ->> 'error' END AS last_error
			FROM river_job
			WHERE ($1 = '' OR state::text = $1)
			  AND ($2 = '' OR kind = $2)
			ORDER BY id DESC
			LIMIT $3`, input.State, input.Kind, input.Limit)
		if err != nil {
			return nil, huma.Error503ServiceUnavailable("job queue is unavailable")
		}
		defer jobRows.Close()
		for jobRows.Next() {
			var job adminJob
			var attempt, maxAttempts, priority int16
			var args []byte
			var lastError *string
			if err := jobRows.Scan(
				&job.ID, &job.Kind, &job.State, &job.Queue, &attempt, &maxAttempts, &priority,
				&job.CreatedAt, &job.ScheduledAt, &job.AttemptedAt, &job.FinalizedAt, &args, &lastError,
			); err != nil {
				return nil, huma.Error503ServiceUnavailable("job queue is unavailable")
			}
			job.Attempt, job.MaxAttempts, job.Priority = int(attempt), int(maxAttempts), int(priority)
			if len(args) > 0 {
				job.Args = json.RawMessage(args)
			}
			if lastError != nil {
				job.Error = *lastError
			}
			body.Jobs = append(body.Jobs, job)
		}
		if err := jobRows.Err(); err != nil {
			return nil, huma.Error503ServiceUnavailable("job queue is unavailable")
		}

		return &adminJobsOutput{CacheControl: "no-store", Body: body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "admin-job-action",
		Method:        http.MethodPost,
		Path:          "/api/v2/admin/jobs/actions",
		Summary:       "Bulk-manage the River job queue",
		Description:   "clear_completed deletes finished job history; clear_queue deletes waiting (available/scheduled/retryable/pending) jobs; rescue_stuck requeues jobs stuck running past the abandonment threshold. Admin role required.",
		Tags:          []string{"Admin"},
		DefaultStatus: http.StatusOK,
		Errors:        []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable},
	}, func(ctx context.Context, input *adminJobActionInput) (*adminJobActionOutput, error) {
		if service == nil || runtime == nil {
			return nil, huma.Error503ServiceUnavailable("admin service is unavailable")
		}
		if _, err := requireAdmin(ctx, service, input.Session, input.Authorization); err != nil {
			return nil, err
		}

		var sql string
		switch input.Body.Action {
		case "clear_completed":
			sql = `DELETE FROM river_job WHERE state = 'completed'`
		case "clear_queue":
			sql = `DELETE FROM river_job WHERE state IN ('available', 'scheduled', 'retryable', 'pending')`
		case "rescue_stuck":
			sql = `UPDATE river_job
			        SET state = 'available', scheduled_at = now(), attempted_at = NULL, attempted_by = NULL
			        WHERE state = 'running' AND (attempted_at IS NULL OR attempted_at < now() - interval '` + stuckRunningInterval + `')`
		default:
			return nil, huma.Error400BadRequest("unknown action")
		}

		tag, err := runtime.DB.Exec(ctx, sql)
		if err != nil {
			return nil, huma.Error503ServiceUnavailable("job queue action failed")
		}
		out := &adminJobActionOutput{CacheControl: "no-store"}
		out.Body.Action = input.Body.Action
		out.Body.Affected = tag.RowsAffected()
		return out, nil
	})
}

// requireAdmin resolves the caller and enforces the admin role, returning the
// standard 401/403 problems otherwise.
func requireAdmin(ctx context.Context, service *auth.Service, session, authorization string) (auth.User, error) {
	user, err := currentAuthUser(ctx, service, session, authorization)
	if err != nil {
		return auth.User{}, authHTTPError(ctx, "load current user", err)
	}
	if user.Role != "admin" {
		return auth.User{}, huma.Error403Forbidden("admin access required")
	}
	return user, nil
}
