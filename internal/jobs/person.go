package jobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/people"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const PersonEnrichKind = "person_enrich_v1"

type PersonEnrichArgs struct {
	EntityID      string `json:"entity_id" river:"unique"`
	TMDBID        string `json:"tmdb_id,omitempty"`
	TVMazeID      string `json:"tvmaze_id,omitempty"`
	TVDBID        string `json:"tvdb_id,omitempty"`
	CredentialRef string `json:"credential_ref,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

func (PersonEnrichArgs) Kind() string { return PersonEnrichKind }
func (PersonEnrichArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, Priority: PriorityStaleRead, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}

func InsertPersonEnrich(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], args PersonEnrichArgs, priority int) (*rivertype.JobInsertResult, error) {
	inserted, err := client.Insert(ctx, args, &river.InsertOpts{Priority: priority})
	if err != nil {
		return nil, err
	}
	tag, err := runtime.DB.Exec(ctx, `UPDATE river_job SET priority=LEAST(priority,$2),args=CASE WHEN $4='' THEN CASE WHEN $3='' THEN args ELSE jsonb_set(args,'{credential_ref}',to_jsonb($3::text),true)END ELSE jsonb_set(CASE WHEN $3='' THEN args ELSE jsonb_set(args,'{credential_ref}',to_jsonb($3::text),true)END,'{reason}',to_jsonb($4::text),true)END WHERE id=$1 AND state IN('available','pending','retryable','scheduled')`, inserted.Job.ID, priority, args.CredentialRef, args.Reason)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 && args.CredentialRef != "" {
		_ = providercredentials.Delete(context.WithoutCancel(ctx), runtime.Redis, args.CredentialRef)
	}
	for provider, value := range map[string]string{"tmdb": args.TMDBID, "tvmaze": args.TVMazeID, "tvdb": args.TVDBID} {
		if value == "" {
			continue
		}
		_, _ = runtime.DB.Exec(ctx, `UPDATE river_job SET args=jsonb_set(args,$2,to_jsonb($3::text),true) WHERE id=$1 AND state IN('available','pending','retryable','scheduled')`, inserted.Job.ID, []string{provider + "_id"}, value)
		_, _ = runtime.DB.Exec(ctx, `INSERT INTO provider_refresh_states(entity_id,provider,last_attempt_at,current_job_id,next_eligible_at)VALUES($1,$2,now(),$3,now())ON CONFLICT(entity_id,provider)DO UPDATE SET last_attempt_at=now(),current_job_id=$3`, args.EntityID, provider, inserted.Job.ID)
	}
	return inserted, nil
}

type PersonEnrichWorker struct {
	river.WorkerDefaults[PersonEnrichArgs]
	service *people.Service
	runtime *platform.Runtime
}

func NewPersonEnrichWorker(runtime *platform.Runtime) *PersonEnrichWorker {
	return &PersonEnrichWorker{service: people.NewService(runtime), runtime: runtime}
}

func (w *PersonEnrichWorker) Work(ctx context.Context, job *river.Job[PersonEnrichArgs]) error {
	credentials, err := providercredentials.Load(ctx, w.runtime.Redis, job.Args.CredentialRef)
	if err != nil {
		return river.JobCancel(err)
	}
	var enrichmentErrors []error
	if job.Args.TMDBID != "" {
		if enrichErr := w.service.EnrichTMDB(ctx, job.Args.EntityID, job.Args.TMDBID, job.ID, credentials); enrichErr != nil {
			enrichmentErrors = append(enrichmentErrors, enrichErr)
		}
	}
	if job.Args.TVMazeID != "" {
		if enrichErr := w.service.EnrichTVMaze(ctx, job.Args.EntityID, job.Args.TVMazeID, job.ID); enrichErr != nil {
			enrichmentErrors = append(enrichmentErrors, enrichErr)
		}
	}
	if job.Args.TVDBID != "" {
		if enrichErr := w.service.EnrichTVDB(ctx, job.Args.EntityID, job.Args.TVDBID, job.ID, credentials); enrichErr != nil {
			enrichmentErrors = append(enrichmentErrors, enrichErr)
		}
	}
	err = errors.Join(enrichmentErrors...)
	if err == nil {
		_ = providercredentials.Delete(context.WithoutCancel(ctx), w.runtime.Redis, job.Args.CredentialRef)
		return nil
	}
	var status *providers.StatusError
	if errors.As(err, &status) {
		if status.StatusCode == http.StatusNotFound {
			_ = providercredentials.Delete(context.WithoutCancel(ctx), w.runtime.Redis, job.Args.CredentialRef)
			return river.JobCancel(err)
		}
		if status.StatusCode == http.StatusTooManyRequests {
			return river.JobSnooze(5 * time.Minute)
		}
	}
	return fmt.Errorf("enrich canonical person %s: %w", job.Args.EntityID, err)
}
