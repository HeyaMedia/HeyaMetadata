package discovery

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var musicBrainzIdentifierPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type claimTarget struct {
	EntityKind string
	Provider   string
	Namespace  string
	ViaWork    bool
}

// ResolveKnownIdentifiers is a local-only identity check. It never contacts an
// upstream provider and therefore belongs on the synchronous API path before a
// durable discovery job is scheduled.
func (s *Service) ResolveKnownIdentifiers(ctx context.Context, request Request) (Result, bool, error) {
	request = NormalizeRequest(request)
	if len(request.Identifiers) == 0 {
		return Result{}, false, nil
	}
	result := baseIdentifierResult(request)
	entityEvidence := map[string][]int{}
	hasSupportedUnresolved := hasArtistReleaseIdentityEvidence(request)
	for index, identifier := range request.Identifiers {
		evidence := IdentifierEvidence{Scheme: identifier.Scheme, Value: identifier.Value}
		target, supported := claimTargetFor(request.Kind, identifier)
		if !supported {
			evidence.Outcome = "unsupported"
			evidence.Detail = "identifier scheme is not mapped for this media kind"
			result.IdentifierEvidence = append(result.IdentifierEvidence, evidence)
			continue
		}
		if !ValidIdentifierValue(identifier) {
			evidence.Outcome = "unused"
			evidence.Detail = "identifier value is invalid for scheme"
			result.IdentifierEvidence = append(result.IdentifierEvidence, evidence)
			continue
		}
		entityID, err := s.resolveClaim(ctx, target, identifier.Value)
		if errors.Is(err, pgx.ErrNoRows) {
			evidence.Outcome = "unused"
			evidence.Detail = "identifier is valid evidence but is not known locally yet"
			result.IdentifierEvidence = append(result.IdentifierEvidence, evidence)
			hasSupportedUnresolved = true
			continue
		}
		if err != nil {
			return Result{}, false, err
		}
		evidence.Outcome = "resolved"
		result.IdentifierEvidence = append(result.IdentifierEvidence, evidence)
		entityEvidence[entityID] = append(entityEvidence[entityID], index)
	}
	if len(entityEvidence) == 0 {
		return result, false, nil
	}
	if len(entityEvidence) == 1 {
		for entityID, indexes := range entityEvidence {
			result.EntityID = entityID
			result.Recommendation = "existing_entity"
			for _, index := range indexes[1:] {
				result.IdentifierEvidence[index].Outcome = "corroborating"
			}
		}
		// A locally known identifier cannot prove that fresh supported evidence
		// names the same entity. Keep the canonical candidate as context, but let
		// the durable worker crosswalk and ingest the unresolved identifiers
		// before returning a final identity.
		return result, !hasSupportedUnresolved, nil
	}
	result.Status = "needs_selection"
	result.Recommendation = "conflicting_identifiers"
	entityIDs := make([]string, 0, len(entityEvidence))
	for entityID, indexes := range entityEvidence {
		entityIDs = append(entityIDs, entityID)
		for _, index := range indexes {
			result.IdentifierEvidence[index].Outcome = "conflict"
			result.IdentifierEvidence[index].Detail = "identifier resolves to a different canonical Heya entity"
		}
	}
	sort.Strings(entityIDs)
	for index, entityID := range entityIDs {
		display, err := s.canonicalCandidateDisplay(ctx, entityID)
		if err != nil {
			return Result{}, false, err
		}
		result.Candidates = append(result.Candidates, Candidate{
			Rank: index + 1, Confidence: 1, Match: "conflict", Display: display,
			Evidence:   []Evidence{{Field: "identifiers", Outcome: "conflict", Weight: 1, Detail: "provided identifiers disagree"}},
			Resolution: Resolution{Kind: request.Kind, Provider: "heya", Namespace: "entity", Value: entityID},
		})
	}
	return result, true, nil
}

func hasArtistReleaseIdentityEvidence(request Request) bool {
	if request.Kind != KindArtist {
		return false
	}
	for _, release := range request.Hints.Releases {
		for _, identifier := range release.Identifiers {
			if !ValidIdentifierValue(identifier) {
				continue
			}
			switch identifier.Scheme {
			case "musicbrainz", "apple", "deezer", "discogs_release", "discogs_master":
				return true
			}
		}
	}
	return false
}

// ValidIdentifierValue performs the cheap, scheme-specific validation needed
// before caller evidence can reach a provider client, cache key, or typed
// identity column. Unknown schemes remain syntactically valid so they can be
// reported as unsupported by the media-kind routing layer.
func ValidIdentifierValue(identifier Identifier) bool {
	value := strings.TrimSpace(identifier.Value)
	if value == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(identifier.Scheme)) {
	case "musicbrainz":
		return musicBrainzIdentifierPattern.MatchString(strings.ToLower(value))
	case "apple", "deezer", "discogs", "discogs_release", "discogs_master":
		number, err := strconv.ParseInt(value, 10, 64)
		return err == nil && number > 0
	default:
		return true
	}
}

func baseIdentifierResult(request Request) Result {
	return Result{
		SchemaVersion:  SchemaVersion,
		Kind:           request.Kind,
		Query:          request.Query,
		Status:         "completed",
		Recommendation: "no_match",
		Candidates:     []Candidate{},
		ObservedAt:     time.Now().UTC(),
	}
}

func (s *Service) resolveClaim(ctx context.Context, target claimTarget, value string) (string, error) {
	var entityID string
	if target.ViaWork {
		err := s.runtime.DB.QueryRow(ctx, `SELECT edition.work_entity_id::text FROM external_id_claims claim JOIN canonical_book_editions edition ON edition.entity_id=claim.entity_id JOIN entities work ON work.id=edition.work_entity_id AND work.kind=$1 AND work.deleted_at IS NULL WHERE claim.entity_kind=$2 AND claim.provider=$3 AND claim.namespace=$4 AND claim.normalized_value=$5 AND claim.state='accepted'`, target.EntityKind, editionKindFor(target.EntityKind), target.Provider, target.Namespace, value).Scan(&entityID)
		return entityID, err
	}
	err := s.runtime.DB.QueryRow(ctx, `SELECT claim.entity_id::text FROM external_id_claims claim JOIN entities entity ON entity.id=claim.entity_id AND entity.kind=$1 AND entity.deleted_at IS NULL WHERE claim.entity_kind=$1 AND claim.provider=$2 AND claim.namespace=$3 AND claim.normalized_value=$4 AND claim.state='accepted'`, target.EntityKind, target.Provider, target.Namespace, value).Scan(&entityID)
	return entityID, err
}

func (s *Service) canonicalCandidateDisplay(ctx context.Context, entityID string) (Display, error) {
	var title string
	var year int
	err := s.runtime.DB.QueryRow(ctx, `SELECT display_title,COALESCE(release_year,0) FROM search_entities WHERE entity_id=$1`, entityID).Scan(&title, &year)
	if errors.Is(err, pgx.ErrNoRows) {
		return Display{}, nil
	}
	if err != nil {
		return Display{}, err
	}
	return Display{Title: title, Name: title, Year: year}, nil
}

func claimTargetFor(kind string, identifier Identifier) (claimTarget, bool) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	scheme := identifier.Scheme
	target := claimTarget{EntityKind: kind}
	switch scheme {
	case "musicbrainz":
		target.Provider = "musicbrainz"
		target.Namespace = map[string]string{KindArtist: "artist", KindReleaseGroup: "release_group", "release": "release", KindRecording: "recording"}[kind]
	case "tmdb":
		target.Provider = "tmdb"
		target.Namespace = map[string]string{KindMovie: "movie", KindTVShow: "tv", KindAnime: "tv", "person": "person"}[kind]
	case "imdb":
		target.Provider = "imdb"
		target.Namespace = map[string]string{KindMovie: "title", KindTVShow: "title", KindAnime: "title", KindArtist: "name", "person": "name"}[kind]
	case "tvdb":
		target.Provider = "tvdb"
		target.Namespace = map[string]string{KindMovie: "movie", KindTVShow: "series", KindAnime: "series", "person": "person"}[kind]
	case "tvmaze":
		target.Provider = "tvmaze"
		target.Namespace = map[string]string{KindTVShow: "show", KindAnime: "show", "person": "person"}[kind]
	case "tvrage":
		target.Provider, target.Namespace = "tvrage", "show"
	case "anidb":
		target.Provider, target.Namespace = "anidb", "anime"
	case "anilist":
		target.Provider = "anilist"
		target.Namespace = map[string]string{KindAnime: "anime", KindManga: "manga"}[kind]
	case "myanimelist":
		target.Provider = "myanimelist"
		target.Namespace = map[string]string{KindAnime: "anime", KindManga: "manga"}[kind]
	case "kitsu":
		target.Provider, target.Namespace = "kitsu", "manga"
	case "openlibrary":
		target.Provider = "openlibrary"
		suffix := strings.ToUpper(identifier.Value)
		switch {
		case strings.HasSuffix(suffix, "W"):
			target.Namespace = "work"
		case strings.HasSuffix(suffix, "M"):
			target.Namespace = "edition"
			if editionKindFor(kind) != "" {
				target.ViaWork = true
			} else {
				target.EntityKind = kind
			}
		case strings.HasSuffix(suffix, "A"):
			target.EntityKind, target.Namespace = "author", "author"
		}
	case "isbn":
		target.Provider = "isbn"
		if len(identifier.Value) == 10 {
			target.Namespace = "isbn10"
		} else if len(identifier.Value) == 13 {
			target.Namespace = "isbn13"
		}
		if target.Namespace != "" && (kind == KindBookWork || kind == KindMangaVolume || kind == KindComicVolume) {
			target.ViaWork = true
		}
	case "googlebooks":
		target.Provider, target.Namespace = "googlebooks", "volume"
		if editionKindFor(kind) != "" {
			target.ViaWork = true
		}
	case "apple", "deezer", "discogs", "spotify":
		target.Provider = scheme
		target.Namespace = musicNamespace(kind, scheme)
	case "isrc":
		target.Provider, target.Namespace = "isrc", "recording"
	case "openopus":
		target.Provider, target.Namespace = "openopus", "work"
	}
	if target.EntityKind == "" || target.Provider == "" || target.Namespace == "" {
		return claimTarget{}, false
	}
	return target, true
}

func editionKindFor(workKind string) string {
	return map[string]string{KindBookWork: "book_edition", KindMangaVolume: "manga_edition", KindComicVolume: "comic_edition"}[workKind]
}

func musicNamespace(kind, provider string) string {
	switch kind {
	case KindArtist:
		return "artist"
	case KindReleaseGroup:
		if provider == "discogs" {
			return "master"
		}
		return "album"
	case "release":
		if provider == "discogs" {
			return "release"
		}
		return "album"
	case KindRecording:
		return "track"
	}
	return ""
}

func (r Result) ValidateCanonicalOutcome() error {
	if r.EntityID != "" && len(r.Candidates) > 0 {
		return fmt.Errorf("canonical discovery result cannot contain both entity and candidates")
	}
	return nil
}
