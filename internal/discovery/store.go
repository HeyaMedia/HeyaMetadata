package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/jackc/pgx/v5"
	"time"
)

type Run struct {
	ID          string    `json:"id"`
	RequestHash string    `json:"request_hash"`
	State       string    `json:"state"`
	Request     Request   `json:"request"`
	Result      *Result   `json:"result,omitempty"`
	RiverJobID  int64     `json:"river_job_id,omitempty"`
	Error       string    `json:"error,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func RequestHash(request Request) (string, []byte, error) {
	request = NormalizeRequest(request)
	body, err := json.Marshal(request)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), body, nil
}
func EnsureRun(ctx context.Context, runtime *platform.Runtime, request Request) (Run, error) {
	request = NormalizeRequest(request)
	hash, body, err := RequestHash(request)
	if err != nil {
		return Run{}, err
	}
	if cached, cacheErr := runtime.Redis.Get(ctx, discoveryCacheKey(hash)).Bytes(); cacheErr == nil {
		var run Run
		if json.Unmarshal(cached, &run) == nil && run.State == "completed" && run.ExpiresAt.After(time.Now()) {
			return run, nil
		}
	}
	var run Run
	var requestJSON, document []byte
	var jobID *int64
	err = runtime.DB.QueryRow(ctx, `INSERT INTO discovery_runs (request_hash,kind,query,request,state,expires_at) VALUES ($1,$2,$3,$4,'queued',now()+interval '6 hours') ON CONFLICT (request_hash) DO UPDATE SET kind=EXCLUDED.kind,query=EXCLUDED.query,request=EXCLUDED.request,state='queued',river_job_id=NULL,document=NULL,error=NULL,updated_at=now(),completed_at=NULL,expires_at=EXCLUDED.expires_at WHERE discovery_runs.expires_at<=now() RETURNING id,request_hash,state,request,document,river_job_id,COALESCE(error,''),expires_at`, hash, request.Kind, request.Query, body).Scan(&run.ID, &run.RequestHash, &run.State, &requestJSON, &document, &jobID, &run.Error, &run.ExpiresAt)
	if err == pgx.ErrNoRows {
		run, getErr := GetRunByHash(ctx, runtime, hash)
		if getErr == nil && run.State == "completed" {
			cacheRun(ctx, runtime, run)
		}
		return run, getErr
	}
	if err != nil {
		return Run{}, fmt.Errorf("ensure discovery run: %w", err)
	}
	_ = json.Unmarshal(requestJSON, &run.Request)
	if len(document) > 0 {
		var result Result
		if json.Unmarshal(document, &result) == nil {
			run.Result = &result
		}
	}
	if jobID != nil {
		run.RiverJobID = *jobID
	}
	return run, nil
}
func GetRun(ctx context.Context, runtime *platform.Runtime, id string) (Run, error) {
	return getRun(ctx, runtime, `id=$1`, id)
}
func GetRunByHash(ctx context.Context, runtime *platform.Runtime, hash string) (Run, error) {
	return getRun(ctx, runtime, `request_hash=$1`, hash)
}
func getRun(ctx context.Context, runtime *platform.Runtime, where, value string) (Run, error) {
	var run Run
	var requestJSON, document []byte
	var jobID *int64
	query := `SELECT id,request_hash,state,request,document,river_job_id,COALESCE(error,''),expires_at FROM discovery_runs WHERE ` + where
	if err := runtime.DB.QueryRow(ctx, query, value).Scan(&run.ID, &run.RequestHash, &run.State, &requestJSON, &document, &jobID, &run.Error, &run.ExpiresAt); err != nil {
		return Run{}, err
	}
	_ = json.Unmarshal(requestJSON, &run.Request)
	if len(document) > 0 {
		var result Result
		if json.Unmarshal(document, &result) == nil {
			run.Result = &result
		}
	}
	if jobID != nil {
		run.RiverJobID = *jobID
	}
	return run, nil
}
func AttachJob(ctx context.Context, runtime *platform.Runtime, hash string, jobID int64) error {
	_, err := runtime.DB.Exec(ctx, `UPDATE discovery_runs SET river_job_id=$2,updated_at=now() WHERE request_hash=$1 AND state='queued'`, hash, jobID)
	return err
}
func Start(ctx context.Context, runtime *platform.Runtime, hash string) error {
	_, err := runtime.DB.Exec(ctx, `UPDATE discovery_runs SET state='working',updated_at=now(),error=NULL WHERE request_hash=$1`, hash)
	return err
}
func Complete(ctx context.Context, runtime *platform.Runtime, hash string, result Result) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = runtime.DB.Exec(ctx, `UPDATE discovery_runs SET state='completed',document=$2,error=NULL,completed_at=now(),updated_at=now(),expires_at=now()+interval '6 hours' WHERE request_hash=$1`, hash, body)
	if err != nil {
		return err
	}
	if run, getErr := GetRunByHash(ctx, runtime, hash); getErr == nil {
		cacheRun(ctx, runtime, run)
	}
	return nil
}
func Fail(ctx context.Context, runtime *platform.Runtime, hash string, failure error) {
	_, _ = runtime.DB.Exec(context.WithoutCancel(ctx), `UPDATE discovery_runs SET state='failed',error=$2,completed_at=now(),updated_at=now(),expires_at=now()+interval '10 minutes' WHERE request_hash=$1`, hash, failure.Error())
}
func discoveryCacheKey(hash string) string { return "heya:metadata:v2:discovery:" + hash }
func cacheRun(ctx context.Context, runtime *platform.Runtime, run Run) {
	body, err := json.Marshal(run)
	if err != nil {
		return
	}
	ttl := time.Until(run.ExpiresAt)
	if ttl > 0 {
		_ = runtime.Redis.Set(ctx, discoveryCacheKey(run.RequestHash), body, ttl).Err()
	}
}
