package people

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/jackc/pgx/v5"
)

var ErrNotFound = errors.New("person not found")

type PersonExternalID struct {
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}

type PersonDisplay struct {
	Title   string `json:"title"`
	ImageID string `json:"image_id,omitempty"`
}

type PersonCredit struct {
	EntityID         string `json:"entity_id,omitempty" format:"uuid"`
	ProviderTargetID string `json:"provider_target_id,omitempty"`
	Kind             string `json:"kind"`
	Title            string `json:"title"`
	Year             int    `json:"year,omitempty"`
	ImageID          string `json:"image_id,omitempty" format:"uuid"`
	CreditType       string `json:"credit_type"`
	Character        string `json:"character,omitempty"`
	Department       string `json:"department,omitempty"`
	Job              string `json:"job,omitempty"`
	Order            int    `json:"order,omitempty"`
	Provider         string `json:"provider"`
	ResolutionState  string `json:"resolution_state" enum:"materialized,unresolved"`
}

type PersonData struct {
	Names              []string          `json:"names"`
	Biography          string            `json:"biography,omitempty"`
	BirthDate          string            `json:"birth_date,omitempty"`
	DeathDate          string            `json:"death_date,omitempty"`
	Gender             string            `json:"gender,omitempty"`
	PlaceOfBirth       string            `json:"place_of_birth,omitempty"`
	KnownForDepartment string            `json:"known_for_department,omitempty"`
	Homepage           string            `json:"homepage,omitempty"`
	Popularity         float64           `json:"popularity,omitempty"`
	Biographies        map[string]string `json:"biographies,omitempty"`
	Credits            []PersonCredit    `json:"credits"`
	CreditTotal        int               `json:"credit_total"`
}

type PersonDocument struct {
	SchemaVersion     int                `json:"schema_version"`
	ProjectionVersion int64              `json:"projection_version"`
	ID                string             `json:"id" format:"uuid"`
	Kind              string             `json:"kind"`
	Slug              string             `json:"slug"`
	Display           PersonDisplay      `json:"display"`
	ExternalIDs       []PersonExternalID `json:"external_ids"`
	Data              PersonData         `json:"data"`
	Freshness         PersonFreshness    `json:"freshness"`
}

type PersonFreshness struct {
	State      string     `json:"state"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
	FreshUntil *time.Time `json:"fresh_until,omitempty"`
}

type Service struct{ runtime *platform.Runtime }

func NewService(runtime *platform.Runtime) *Service { return &Service{runtime: runtime} }

func (s *Service) Resolve(ctx context.Context, provider, providerPersonID string) (string, error) {
	var entityID string
	err := s.runtime.DB.QueryRow(ctx, `SELECT entity_id::text FROM external_id_claims WHERE entity_kind='person' AND provider=$1 AND namespace='person' AND normalized_value=$2 AND state='accepted'`, strings.ToLower(strings.TrimSpace(provider)), strings.TrimSpace(providerPersonID)).Scan(&entityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return entityID, err
}

func (s *Service) CanonicalID(ctx context.Context, id string) (string, error) {
	var canonicalID string
	err := s.runtime.DB.QueryRow(ctx, `WITH RECURSIVE chain(id,depth)AS(SELECT $1::uuid,0 UNION ALL SELECT redirect.survivor_entity_id,chain.depth+1 FROM chain JOIN entity_redirects redirect ON redirect.retired_entity_id=chain.id WHERE chain.depth<16)SELECT chain.id::text FROM chain JOIN entities entity ON entity.id=chain.id WHERE entity.kind='person' AND entity.deleted_at IS NULL ORDER BY chain.depth DESC LIMIT 1`, id).Scan(&canonicalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return canonicalID, err
}

func (s *Service) Detail(ctx context.Context, id string) (PersonDocument, error) {
	id, err := s.CanonicalID(ctx, id)
	if err != nil {
		return PersonDocument{}, err
	}
	var result PersonDocument
	result.SchemaVersion, result.Kind = 1, "person"
	result.Data.Names, result.Data.Credits, result.ExternalIDs = []string{}, []PersonCredit{}, []PersonExternalID{}
	var biography, birthDate, deathDate, gender, placeOfBirth, knownFor, homepage *string
	var biographies []byte
	err = s.runtime.DB.QueryRow(ctx, `SELECT e.id::text,e.slug,e.canonical_version,p.display_name,COALESCE(p.profile_image_id::text,''),p.biography,p.birth_date::text,p.death_date::text,p.gender,p.place_of_birth,p.known_for_department,p.homepage,COALESCE(p.popularity,0),p.biographies FROM canonical_people p JOIN entities e ON e.id=p.entity_id AND e.kind='person' AND e.deleted_at IS NULL WHERE p.entity_id=$1`, id).Scan(&result.ID, &result.Slug, &result.ProjectionVersion, &result.Display.Title, &result.Display.ImageID, &biography, &birthDate, &deathDate, &gender, &placeOfBirth, &knownFor, &homepage, &result.Data.Popularity, &biographies)
	if errors.Is(err, pgx.ErrNoRows) {
		return PersonDocument{}, ErrNotFound
	}
	if err != nil {
		return PersonDocument{}, err
	}
	if biography != nil {
		result.Data.Biography = *biography
	}
	if birthDate != nil {
		result.Data.BirthDate = *birthDate
	}
	if deathDate != nil {
		result.Data.DeathDate = *deathDate
	}
	if gender != nil {
		result.Data.Gender = *gender
	}
	if placeOfBirth != nil {
		result.Data.PlaceOfBirth = *placeOfBirth
	}
	if knownFor != nil {
		result.Data.KnownForDepartment = *knownFor
	}
	if homepage != nil {
		result.Data.Homepage = *homepage
	}
	_ = json.Unmarshal(biographies, &result.Data.Biographies)
	result.Freshness.State = "stale"
	var refreshedAt, freshUntil *time.Time
	var allFresh bool
	_ = s.runtime.DB.QueryRow(ctx, `SELECT max(refresh.last_success_at),min(refresh.next_eligible_at),COALESCE(bool_and(refresh.next_eligible_at>now()),false) FROM external_id_claims claim LEFT JOIN provider_refresh_states refresh ON refresh.entity_id=claim.entity_id AND refresh.provider=claim.provider WHERE claim.entity_id=$1 AND claim.entity_kind='person' AND claim.namespace='person' AND claim.provider IN('tmdb','tvmaze','tvdb') AND claim.state='accepted'`, id).Scan(&refreshedAt, &freshUntil, &allFresh)
	if refreshedAt != nil {
		result.Freshness.UpdatedAt = refreshedAt
	}
	if freshUntil != nil {
		result.Freshness.FreshUntil = freshUntil
		if allFresh {
			result.Freshness.State = "fresh"
		}
	}

	claimRows, err := s.runtime.DB.Query(ctx, `SELECT provider,namespace,normalized_value FROM external_id_claims WHERE entity_id=$1 AND entity_kind='person' AND state='accepted' ORDER BY provider,namespace,normalized_value`, id)
	if err != nil {
		return PersonDocument{}, err
	}
	defer claimRows.Close()
	for claimRows.Next() {
		var value PersonExternalID
		if err := claimRows.Scan(&value.Provider, &value.Namespace, &value.Value); err != nil {
			return PersonDocument{}, err
		}
		result.ExternalIDs = append(result.ExternalIDs, value)
	}
	if err := claimRows.Err(); err != nil {
		return PersonDocument{}, err
	}

	nameRows, err := s.runtime.DB.Query(ctx, `SELECT DISTINCT value FROM search_names WHERE entity_id=$1 ORDER BY value`, id)
	if err != nil {
		return PersonDocument{}, err
	}
	defer nameRows.Close()
	for nameRows.Next() {
		var name string
		if err := nameRows.Scan(&name); err != nil {
			return PersonDocument{}, err
		}
		result.Data.Names = append(result.Data.Names, name)
	}
	if err := nameRows.Err(); err != nil {
		return PersonDocument{}, err
	}

	credits, err := s.allCredits(ctx, id)
	if err != nil {
		return PersonDocument{}, err
	}
	result.Data.CreditTotal = len(credits)
	if len(credits) > 250 {
		credits = credits[:250]
	}
	result.Data.Credits = credits
	return result, nil
}

func (s *Service) Credits(ctx context.Context, id string, offset, limit int) ([]PersonCredit, int, error) {
	canonicalID, err := s.CanonicalID(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	id = canonicalID
	credits, err := s.allCredits(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	total := len(credits)
	if offset >= total {
		return []PersonCredit{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return credits[offset:end], total, nil
}

func (s *Service) NeedsTMDBRefresh(ctx context.Context, id string) (bool, error) {
	var due bool
	err := s.runtime.DB.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM provider_refresh_states WHERE entity_id=$1 AND provider='tmdb' AND next_eligible_at>now())`, id).Scan(&due)
	return due, err
}

func (s *Service) NeedsEnrichment(ctx context.Context, id string) (bool, error) {
	var due bool
	err := s.runtime.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM external_id_claims claim LEFT JOIN provider_refresh_states refresh ON refresh.entity_id=claim.entity_id AND refresh.provider=claim.provider WHERE claim.entity_id=$1 AND claim.entity_kind='person' AND claim.namespace='person' AND claim.provider IN('tmdb','tvmaze','tvdb') AND claim.state='accepted' AND (refresh.entity_id IS NULL OR refresh.next_eligible_at<=now()))`, id).Scan(&due)
	return due, err
}

func (s *Service) ProviderIDs(ctx context.Context, id string) (map[string]string, error) {
	rows, err := s.runtime.DB.Query(ctx, `SELECT provider,normalized_value FROM external_id_claims WHERE entity_id=$1 AND entity_kind='person' AND namespace='person' AND provider IN('tmdb','tvmaze','tvdb') AND state='accepted'`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var provider, value string
		if err := rows.Scan(&provider, &value); err != nil {
			return nil, err
		}
		result[provider] = value
	}
	return result, rows.Err()
}

func (s *Service) DueProviderIDs(ctx context.Context, id string) (map[string]string, error) {
	rows, err := s.runtime.DB.Query(ctx, `SELECT claim.provider,claim.normalized_value FROM external_id_claims claim LEFT JOIN provider_refresh_states refresh ON refresh.entity_id=claim.entity_id AND refresh.provider=claim.provider WHERE claim.entity_id=$1 AND claim.entity_kind='person' AND claim.namespace='person' AND claim.provider IN('tmdb','tvmaze','tvdb') AND claim.state='accepted' AND (refresh.entity_id IS NULL OR refresh.next_eligible_at<=now())`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var provider, value string
		if err := rows.Scan(&provider, &value); err != nil {
			return nil, err
		}
		result[provider] = value
	}
	return result, rows.Err()
}

func (s *Service) TMDBID(ctx context.Context, id string) (string, error) {
	var value string
	err := s.runtime.DB.QueryRow(ctx, `SELECT normalized_value FROM external_id_claims WHERE entity_id=$1 AND entity_kind='person' AND provider='tmdb' AND namespace='person' AND state='accepted'`, id).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return value, err
}

func (s *Service) allCredits(ctx context.Context, id string) ([]PersonCredit, error) {
	credits := []PersonCredit{}
	seen := map[string]bool{}
	localRows, err := s.runtime.DB.Query(ctx, `SELECT credit.entity_id::text,entity.kind,search.display_title,COALESCE(search.release_year,0),COALESCE(search.summary#>>'{display,image_id}',''),credit.credit_type,COALESCE(credit.character_name,''),COALESCE(credit.department,''),COALESCE(credit.job,''),credit.credit_order,credit.provider FROM entity_credit_projections credit JOIN entities entity ON entity.id=credit.entity_id AND entity.deleted_at IS NULL JOIN search_entities search ON search.entity_id=credit.entity_id WHERE credit.person_entity_id=$1`, id)
	if err != nil {
		return nil, err
	}
	for localRows.Next() {
		var value PersonCredit
		if err := localRows.Scan(&value.EntityID, &value.Kind, &value.Title, &value.Year, &value.ImageID, &value.CreditType, &value.Character, &value.Department, &value.Job, &value.Order, &value.Provider); err != nil {
			localRows.Close()
			return nil, err
		}
		key := creditKey(value)
		value.ResolutionState = "materialized"
		if !seen[key] {
			seen[key] = true
			credits = append(credits, value)
		}
	}
	if err := localRows.Err(); err != nil {
		localRows.Close()
		return nil, err
	}
	localRows.Close()
	providerRows, err := s.runtime.DB.Query(ctx, `SELECT COALESCE((SELECT claim.entity_id::text FROM external_id_claims claim WHERE claim.provider=credit.provider AND claim.normalized_value=credit.provider_target_id AND claim.state='accepted' AND ((credit.provider='tmdb' AND credit.media_kind='movie' AND claim.entity_kind='movie' AND claim.namespace='movie')OR(credit.provider='tmdb' AND credit.media_kind='tv_show' AND claim.entity_kind IN('tv_show','anime') AND claim.namespace='tv')OR(credit.provider='tvmaze' AND credit.media_kind='tv_show' AND claim.entity_kind IN('tv_show','anime') AND claim.namespace='show')OR(credit.provider='tvdb' AND credit.media_kind='tv_show' AND claim.entity_kind IN('tv_show','anime') AND claim.namespace='series')OR(credit.provider='tvdb' AND credit.media_kind='movie' AND claim.entity_kind='movie' AND claim.namespace='movie'))LIMIT 1),''),credit.provider_target_id,credit.media_kind,credit.title,COALESCE(credit.release_year,0),COALESCE(credit.image_id::text,''),credit.credit_type,credit.character_name,credit.department,credit.job,credit.credit_order,credit.provider FROM person_provider_credits credit WHERE credit.person_entity_id=$1`, id)
	if err != nil {
		return nil, err
	}
	defer providerRows.Close()
	for providerRows.Next() {
		var value PersonCredit
		if err := providerRows.Scan(&value.EntityID, &value.ProviderTargetID, &value.Kind, &value.Title, &value.Year, &value.ImageID, &value.CreditType, &value.Character, &value.Department, &value.Job, &value.Order, &value.Provider); err != nil {
			return nil, err
		}
		key := creditKey(value)
		value.ResolutionState = "unresolved"
		if value.EntityID != "" {
			value.ResolutionState = "materialized"
		}
		if !seen[key] {
			seen[key] = true
			credits = append(credits, value)
		}
	}
	if err := providerRows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(credits, func(i, j int) bool {
		if credits[i].Year != credits[j].Year {
			return credits[i].Year > credits[j].Year
		}
		if credits[i].Title != credits[j].Title {
			return credits[i].Title < credits[j].Title
		}
		return credits[i].Order < credits[j].Order
	})
	return credits, nil
}

func creditKey(value PersonCredit) string {
	target := value.EntityID
	if target == "" {
		target = value.Provider + ":" + value.ProviderTargetID
	}
	return strings.Join([]string{target, value.CreditType, value.Character, value.Department, value.Job}, "\x00")
}
