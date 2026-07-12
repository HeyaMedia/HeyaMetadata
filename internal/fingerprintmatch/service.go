package fingerprintmatch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/fingerprint"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/acoustid"
	"github.com/jackc/pgx/v5"
)

type Request struct {
	RawFingerprint      []byte
	AcoustIDFingerprint string
	DurationMS          int64
}
type MatchCandidate struct {
	RecordingID   string           `json:"recording_id,omitempty"`
	MusicBrainzID string           `json:"musicbrainz_id,omitempty"`
	Title         string           `json:"title,omitempty"`
	Artists       []string         `json:"artists,omitempty"`
	Confidence    float64          `json:"confidence"`
	Match         string           `json:"match"`
	Sources       []MatchSource    `json:"sources"`
	Resolution    *MatchResolution `json:"resolution,omitempty"`
}
type MatchSource struct {
	Provider string  `json:"provider"`
	Score    float64 `json:"score"`
	BitError float64 `json:"bit_error,omitempty"`
	Overlap  int     `json:"overlap,omitempty"`
	Offset   int     `json:"offset,omitempty"`
	AcoustID string  `json:"acoustid,omitempty"`
}
type MatchResolution struct {
	Kind      string `json:"kind"`
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}
type MatchResult struct {
	SchemaVersion  int              `json:"schema_version"`
	Status         string           `json:"status"`
	Recommendation string           `json:"recommendation"`
	Candidates     []MatchCandidate `json:"candidates"`
	Providers      []string         `json:"providers"`
	Warnings       []string         `json:"warnings,omitempty"`
	MatchedAt      time.Time        `json:"matched_at"`
}
type Run struct {
	ID          string       `json:"id"`
	RequestHash string       `json:"request_hash"`
	State       string       `json:"state"`
	Result      *MatchResult `json:"result,omitempty"`
	RiverJobID  int64        `json:"river_job_id,omitempty"`
	Error       string       `json:"error,omitempty"`
	ExpiresAt   time.Time    `json:"expires_at"`
}
type Service struct{ runtime *platform.Runtime }

func NewService(r *platform.Runtime) *Service { return &Service{runtime: r} }

func RequestHash(request Request) string {
	sum := sha256.New()
	sum.Write(request.RawFingerprint)
	sum.Write([]byte{0})
	sum.Write([]byte(request.AcoustIDFingerprint))
	sum.Write([]byte(fmt.Sprintf(":%d", request.DurationMS)))
	return hex.EncodeToString(sum.Sum(nil))
}
func EnsureRun(ctx context.Context, r *platform.Runtime, request Request) (Run, error) {
	hash := RequestHash(request)
	var run Run
	var doc []byte
	var job *int64
	err := r.DB.QueryRow(ctx, `INSERT INTO fingerprint_match_runs(request_hash,raw_fingerprint,acoustid_fingerprint,duration_ms,state)VALUES($1,NULLIF($2,''::bytea),NULLIF($3,''),$4,'queued')ON CONFLICT(request_hash)WHERE state IN('queued','working')DO UPDATE SET expires_at=now()+interval '1 hour' RETURNING id,request_hash,state,document,river_job_id,COALESCE(error,''),expires_at`, hash, request.RawFingerprint, request.AcoustIDFingerprint, request.DurationMS).Scan(&run.ID, &run.RequestHash, &run.State, &doc, &job, &run.Error, &run.ExpiresAt)
	if err != nil {
		return run, err
	}
	if len(doc) > 0 {
		var result MatchResult
		if json.Unmarshal(doc, &result) == nil {
			run.Result = &result
		}
	}
	if job != nil {
		run.RiverJobID = *job
	}
	return run, nil
}
func GetRun(ctx context.Context, r *platform.Runtime, id string) (Run, error) {
	var run Run
	var doc []byte
	var job *int64
	err := r.DB.QueryRow(ctx, `SELECT id,request_hash,state,document,river_job_id,COALESCE(error,''),expires_at FROM fingerprint_match_runs WHERE id=$1`, id).Scan(&run.ID, &run.RequestHash, &run.State, &doc, &job, &run.Error, &run.ExpiresAt)
	if err != nil {
		return run, err
	}
	if len(doc) > 0 {
		var result MatchResult
		if json.Unmarshal(doc, &result) == nil {
			run.Result = &result
		}
	}
	if job != nil {
		run.RiverJobID = *job
	}
	return run, nil
}
func AttachJob(ctx context.Context, r *platform.Runtime, id string, jobID int64) error {
	_, err := r.DB.Exec(ctx, `UPDATE fingerprint_match_runs SET river_job_id=$2 WHERE id=$1 AND state='queued'`, id, jobID)
	return err
}
func (s *Service) MatchRun(ctx context.Context, id string, credentials providercredentials.Credentials) (result MatchResult, returnErr error) {
	var raw []byte
	var compressed string
	var duration int64
	if err := s.runtime.DB.QueryRow(ctx, `UPDATE fingerprint_match_runs SET state='working',error=NULL WHERE id=$1 RETURNING raw_fingerprint,COALESCE(acoustid_fingerprint,''),duration_ms`, id).Scan(&raw, &compressed, &duration); err != nil {
		return result, err
	}
	defer func() {
		if returnErr != nil {
			_, _ = s.runtime.DB.Exec(context.WithoutCancel(ctx), `UPDATE fingerprint_match_runs SET state='failed',error=$2,completed_at=now()WHERE id=$1`, id, returnErr.Error())
		}
	}()
	result = MatchResult{SchemaVersion: 1, Status: "completed", MatchedAt: time.Now().UTC()}
	byKey := map[string]*MatchCandidate{}
	if len(raw) > 0 {
		result.Providers = append(result.Providers, "local_chromaprint")
		tokens := fingerprint.LandmarkTokens(raw)
		rows, err := s.runtime.DB.Query(ctx, `SELECT f.recording_entity_id,f.fingerprint,COALESCE(c.document#>>'{display,title}',''),COALESCE((SELECT normalized_value FROM external_id_claims WHERE entity_id=f.recording_entity_id AND provider='musicbrainz'AND namespace='recording'AND state='accepted' LIMIT 1),'')FROM recording_fingerprints f JOIN canonical_recordings c ON c.entity_id=f.recording_entity_id WHERE f.state='ready' AND f.id IN(SELECT fingerprint_id FROM recording_fingerprint_landmarks WHERE token=ANY($1) GROUP BY fingerprint_id ORDER BY count(*) DESC LIMIT 500)`, tokens)
		if err != nil {
			return result, err
		}
		query := fingerprint.Fingerprint{Hashes: fingerprint.Unpack(raw), Duration: float64(duration) / 1000}
		for rows.Next() {
			var entity, mbid, title string
			var packed []byte
			if err = rows.Scan(&entity, &packed, &title, &mbid); err != nil {
				rows.Close()
				return result, err
			}
			matched := fingerprint.Match(query, fingerprint.Fingerprint{Hashes: fingerprint.Unpack(packed)})
			if !matched.Match {
				continue
			}
			score := 1 - matched.BitError
			key := entity
			c := byKey[key]
			if c == nil {
				c = &MatchCandidate{RecordingID: entity, MusicBrainzID: mbid, Title: title}
				byKey[key] = c
			}
			c.Sources = append(c.Sources, MatchSource{Provider: "local_chromaprint", Score: score, BitError: matched.BitError, Overlap: matched.Overlap, Offset: matched.Offset})
			if score > c.Confidence {
				c.Confidence = score
			}
		}
		rows.Close()
	}
	if compressed != "" {
		key := credentials.APIKey("acoustid")
		response, err := acoustid.New(s.runtime.Config.Providers.AcoustID).Lookup(ctx, compressed, int((duration+500)/1000), key)
		if err != nil {
			result.Warnings = append(result.Warnings, err.Error())
		} else {
			result.Providers = append(result.Providers, "acoustid")
			for _, hit := range response.Results {
				for _, recording := range hit.Recordings {
					mbid := strings.ToLower(recording.ID)
					entity := ""
					_ = s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='recording'AND provider='musicbrainz'AND namespace='recording'AND normalized_value=$1 AND state='accepted'`, mbid).Scan(&entity)
					mapKey := entity
					if mapKey == "" {
						mapKey = "mbid:" + mbid
					}
					c := byKey[mapKey]
					if c == nil {
						c = &MatchCandidate{RecordingID: entity, MusicBrainzID: mbid, Title: recording.Title, Resolution: &MatchResolution{Kind: "recording", Provider: "musicbrainz", Namespace: "recording", Value: mbid}}
						if entity != "" {
							c.Resolution = nil
						}
						for _, artist := range recording.Artists {
							c.Artists = append(c.Artists, artist.Name)
						}
						byKey[mapKey] = c
					}
					c.Sources = append(c.Sources, MatchSource{Provider: "acoustid", Score: hit.Score, AcoustID: hit.ID})
					if hit.Score > c.Confidence {
						c.Confidence = hit.Score
					}
				}
			}
		}
	}
	for _, c := range byKey {
		if c.Confidence >= .85 {
			c.Match = "strong"
		} else if c.Confidence >= .65 {
			c.Match = "likely"
		} else {
			c.Match = "possible"
		}
		result.Candidates = append(result.Candidates, *c)
	}
	sort.Slice(result.Candidates, func(i, j int) bool { return result.Candidates[i].Confidence > result.Candidates[j].Confidence })
	if len(result.Candidates) > 20 {
		result.Candidates = result.Candidates[:20]
	}
	result.Recommendation = "no_match"
	if len(result.Candidates) > 0 {
		result.Recommendation = result.Candidates[0].Match + "_match"
	}
	body, _ := json.Marshal(result)
	if _, err := s.runtime.DB.Exec(ctx, `UPDATE fingerprint_match_runs SET state='completed',document=$2,raw_fingerprint=NULL,acoustid_fingerprint=NULL,error=NULL,completed_at=now()WHERE id=$1`, id, body); err != nil {
		return result, err
	}
	return result, nil
}
func Cleanup(ctx context.Context, r *platform.Runtime) error {
	_, err := r.DB.Exec(ctx, `DELETE FROM fingerprint_match_runs WHERE expires_at<=now()`)
	return err
}

var _ = pgx.ErrNoRows
