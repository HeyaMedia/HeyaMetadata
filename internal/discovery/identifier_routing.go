package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/anime"
	"github.com/HeyaMedia/HeyaMetadata/internal/artists"
	"github.com/HeyaMedia/HeyaMetadata/internal/books"
	rgdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
	"github.com/HeyaMedia/HeyaMetadata/internal/episodic"
	"github.com/HeyaMedia/HeyaMetadata/internal/manga"
	"github.com/HeyaMedia/HeyaMetadata/internal/movies"
	"github.com/HeyaMedia/HeyaMetadata/internal/musicalworks"
	"github.com/HeyaMedia/HeyaMetadata/internal/musiccredits"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/animelists"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/apple"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/deezer"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/discogs"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/musicbrainz"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/openlibrary"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tmdb"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tvmaze"
	"github.com/HeyaMedia/HeyaMetadata/internal/recordings"
	"github.com/HeyaMedia/HeyaMetadata/internal/releasegroups"
	"github.com/HeyaMedia/HeyaMetadata/internal/tvshows"
	"github.com/jackc/pgx/v5"
)

type ingestionRoot struct {
	Kind      string
	Provider  string
	Namespace string
	Value     string
}

type artistReleaseCredit struct {
	Root  ingestionRoot
	Names []string
	Role  string
}

// artistReleaseEvidence deliberately keeps one provider release identifier's
// credits together. A collaborative release proves that every listed artist
// participated; it does not claim that those artists are one identity.
type artistReleaseEvidence struct {
	Hint       ReleaseHint
	Identifier Identifier
	Credits    []artistReleaseCredit
}

func (r ingestionRoot) key() string {
	return r.Kind + "\x00" + r.Provider + "\x00" + r.Namespace + "\x00" + r.Value
}

// ResolveFreshIdentifiers crosswalks caller evidence to an internal ingestion
// root and runs that pipeline inside the durable discovery job. Provider names
// never leave this package in the public response.
func (s *Service) ResolveFreshIdentifiers(ctx context.Context, request Request, jobID int64, credentials providercredentials.Credentials) (Result, bool, error) {
	request = NormalizeRequest(request)
	known, handled, err := s.ResolveKnownIdentifiers(ctx, request)
	if err != nil || handled {
		return known, handled, err
	}
	result := known
	knownEntityID := known.EntityID
	if result.Kind == "" {
		result = baseIdentifierResult(request)
	}
	roots := map[string]ingestionRoot{}
	rootEvidence := map[string][]int{}
	anchorRootKeys := map[string]bool{}
	for index, identifier := range request.Identifiers {
		if root, ok := directIngestionRoot(request.Kind, identifier); ok {
			anchorRootKeys[root.key()] = true
		}
		// ResolveKnownIdentifiers has already classified every identifier. Only
		// fresh supported evidence needs an upstream crosswalk; re-routing a known
		// identifier can needlessly ingest a second copy of the same entity.
		if result.IdentifierEvidence[index].Outcome != "unused" || !ValidIdentifierValue(identifier) {
			continue
		}
		values, err := s.rootsForIdentifier(ctx, request.Kind, identifier, jobID, credentials)
		if err != nil {
			return Result{}, false, err
		}
		if len(values) == 0 {
			continue
		}
		result.IdentifierEvidence[index].Outcome = "resolved"
		result.IdentifierEvidence[index].Detail = "identifier mapped to an internal ingestion route"
		for _, root := range values {
			key := root.key()
			roots[key] = root
			anchorRootKeys[key] = true
			rootEvidence[key] = append(rootEvidence[key], index)
		}
	}
	entityIDs := map[string]bool{}
	rootEntities := map[string]string{}
	processedRoots := map[string]bool{}
	if knownEntityID != "" {
		entityIDs[knownEntityID] = true
	}
	resolveRoots := func(values map[string]ingestionRoot) error {
		for _, root := range sortedIngestionRoots(values) {
			key := root.key()
			if processedRoots[key] {
				continue
			}
			processedRoots[key] = true
			entityID, claimErr := s.resolveIngestionRootClaim(ctx, root)
			if errors.Is(claimErr, pgx.ErrNoRows) {
				entityID, claimErr = s.ingestRoot(ctx, root, jobID, credentials)
			}
			if claimErr != nil {
				var status *providers.StatusError
				if errors.As(claimErr, &status) && status.StatusCode == http.StatusNotFound {
					for _, index := range rootEvidence[key] {
						result.IdentifierEvidence[index].Outcome = "unused"
						result.IdentifierEvidence[index].Detail = "upstream identity was not found"
					}
					continue
				}
				return claimErr
			}
			if entityID == "" {
				continue
			}
			rootEntities[key] = entityID
			entityIDs[entityID] = true
		}
		return nil
	}
	if err := resolveRoots(roots); err != nil {
		return Result{}, false, err
	}
	if len(entityIDs) > 1 {
		return s.conflictingIdentifierResult(ctx, request.Kind, result, entityIDs)
	}

	releaseCorroborated := false
	if request.Kind == KindArtist && len(request.Hints.Releases) > 0 {
		releaseEvidence, releaseErr := s.artistReleaseEvidenceFromHints(ctx, request.Hints.Releases, jobID, credentials)
		if releaseErr != nil {
			return Result{}, false, releaseErr
		}
		anchorEntityID := onlyEntityID(entityIDs)
		if anchorEntityID == "" {
			selected := selectUnanchoredArtistReleaseRoots(request, releaseEvidence)
			if len(selected) == 0 {
				if request.Query != "" {
					// Let normal upstream artist discovery use its ranked candidates.
					// A collaborative release alone is not a conflicting identity set.
					return result, false, nil
				}
				return result, true, nil
			}
			for key, root := range selected {
				roots[key] = root
				anchorRootKeys[key] = true
			}
			if err := resolveRoots(selected); err != nil {
				return Result{}, false, err
			}
			if len(entityIDs) > 1 {
				return s.conflictingIdentifierResult(ctx, request.Kind, result, entityIDs)
			}
			anchorEntityID = onlyEntityID(entityIDs)
		}
		if anchorEntityID != "" {
			selected, corroborated, selectionErr := s.selectAnchoredArtistReleaseRoots(ctx, request, releaseEvidence, anchorRootKeys, anchorEntityID)
			if selectionErr != nil {
				return Result{}, false, selectionErr
			}
			releaseCorroborated = corroborated
			for key, root := range selected {
				roots[key] = root
			}
			if err := resolveRoots(selected); err != nil {
				return Result{}, false, err
			}
			if len(entityIDs) > 1 {
				return s.conflictingIdentifierResult(ctx, request.Kind, result, entityIDs)
			}
		}
	}

	if len(entityIDs) == 0 {
		// Exact identifier evidence outranks fuzzy title discovery. If no exact
		// route survived, a textual query can still use ranked discovery.
		if request.Query != "" {
			return result, false, nil
		}
		return result, true, nil
	}
	entityID := onlyEntityID(entityIDs)
	hasResolvedEvidence := knownEntityID != ""
	for _, root := range sortedIngestionRoots(roots) {
		if rootEntities[root.key()] == "" {
			continue
		}
		for _, index := range rootEvidence[root.key()] {
			if hasResolvedEvidence {
				result.IdentifierEvidence[index].Outcome = "corroborating"
				result.IdentifierEvidence[index].Detail = "identifier independently converged on the same canonical Heya entity"
			}
			hasResolvedEvidence = true
		}
	}
	result.EntityID = entityID
	result.Recommendation = "identified"
	if knownEntityID != "" || len(rootEntities) > 1 || releaseCorroborated {
		result.Recommendation = "corroborated_identity"
	}
	result.ObservedAt = time.Now().UTC()
	return result, true, nil
}

func onlyEntityID(values map[string]bool) string {
	if len(values) != 1 {
		return ""
	}
	for value := range values {
		return value
	}
	return ""
}

func (s *Service) conflictingIdentifierResult(ctx context.Context, kind string, result Result, entityIDs map[string]bool) (Result, bool, error) {
	result.EntityID = ""
	result.Status = "needs_selection"
	result.Recommendation = "conflicting_identifiers"
	for index := range result.IdentifierEvidence {
		if result.IdentifierEvidence[index].Outcome == "resolved" || result.IdentifierEvidence[index].Outcome == "corroborating" {
			result.IdentifierEvidence[index].Outcome = "conflict"
			result.IdentifierEvidence[index].Detail = "identifier resolves to a different canonical Heya entity"
		}
	}
	candidateEntityIDs := make([]string, 0, len(entityIDs))
	for candidateEntityID := range entityIDs {
		candidateEntityIDs = append(candidateEntityIDs, candidateEntityID)
	}
	sort.Strings(candidateEntityIDs)
	for index, candidateEntityID := range candidateEntityIDs {
		display, err := s.canonicalCandidateDisplay(ctx, candidateEntityID)
		if err != nil {
			return Result{}, false, err
		}
		candidate := canonicalConflictCandidate(kind, candidateEntityID, display)
		candidate.Rank = index + 1
		result.Candidates = append(result.Candidates, candidate)
	}
	result.ObservedAt = time.Now().UTC()
	return result, true, nil
}

func sortedIngestionRoots(values map[string]ingestionRoot) []ingestionRoot {
	result := make([]ingestionRoot, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	priority := func(root ingestionRoot) int {
		if root.Kind == KindArtist {
			switch root.Provider {
			case "musicbrainz":
				return 0
			case "apple":
				return 1
			case "deezer":
				return 2
			}
		}
		return 10
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := priority(result[i]), priority(result[j])
		if left != right {
			return left < right
		}
		return result[i].key() < result[j].key()
	})
	return result
}

func (s *Service) resolveIngestionRootClaim(ctx context.Context, root ingestionRoot) (string, error) {
	var entityID string
	err := s.runtime.DB.QueryRow(ctx, `SELECT claim.entity_id::text FROM external_id_claims claim JOIN entities entity ON entity.id=claim.entity_id AND entity.kind=$1 AND entity.deleted_at IS NULL WHERE claim.entity_kind=$1 AND claim.provider=$2 AND claim.namespace=$3 AND claim.normalized_value=$4 AND claim.state='accepted'`, root.Kind, root.Provider, root.Namespace, root.Value).Scan(&entityID)
	return entityID, err
}

func (s *Service) artistReleaseEvidenceFromHints(ctx context.Context, hints []ReleaseHint, jobID int64, credentials providercredentials.Credentials) ([]artistReleaseEvidence, error) {
	if len(hints) == 0 {
		return nil, nil
	}
	var musicBrainzClient *musicbrainz.Client
	var appleClient *apple.Client
	var deezerClient *deezer.Client
	var discogsClient *discogs.Client
	loadMusicBrainz := func() error {
		if musicBrainzClient != nil {
			return nil
		}
		base := musicbrainz.New(s.runtime.Config.Providers.MusicBrainz)
		resolver, err := providercache.New(s.runtime, "musicbrainz-artist-release-routing/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
		if err != nil {
			return err
		}
		musicBrainzClient = musicbrainz.NewCached(s.runtime.Config.Providers.MusicBrainz, resolver)
		return nil
	}
	loadApple := func() error {
		if appleClient != nil {
			return nil
		}
		base := apple.New(s.runtime.Config.Providers.Apple)
		resolver, err := providercache.New(s.runtime, "apple-artist-release-routing/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
		if err != nil {
			return err
		}
		appleClient = apple.NewCached(s.runtime.Config.Providers.Apple, resolver, credentials.APIKey("apple"))
		return nil
	}
	loadDeezer := func() error {
		if deezerClient != nil {
			return nil
		}
		base := deezer.New(s.runtime.Config.Providers.Deezer)
		resolver, err := providercache.New(s.runtime, "deezer-artist-release-routing/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
		if err != nil {
			return err
		}
		deezerClient = deezer.NewCached(s.runtime.Config.Providers.Deezer, resolver)
		return nil
	}
	loadDiscogs := func() error {
		if discogsClient != nil {
			return nil
		}
		base := discogs.New(s.runtime.Config.Providers.Discogs)
		resolver, err := providercache.New(s.runtime, "discogs-artist-release-routing/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
		if err != nil {
			return err
		}
		discogsClient = discogs.NewCached(s.runtime.Config.Providers.Discogs, resolver, credentials.APIKey("discogs"))
		return nil
	}
	type pendingLookup struct {
		hint       ReleaseHint
		identifier Identifier
	}
	pending := []pendingLookup{}
	for _, hint := range hints {
		for _, identifier := range hint.Identifiers {
			if !ValidIdentifierValue(identifier) {
				continue
			}
			switch identifier.Scheme {
			case "musicbrainz", "apple", "deezer", "discogs_release", "discogs_master":
				pending = append(pending, pendingLookup{hint: hint, identifier: identifier})
			}
		}
	}
	releasePriority := func(scheme string) int {
		switch scheme {
		case "musicbrainz":
			return 0
		case "apple":
			return 1
		case "deezer":
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(pending, func(i, j int) bool {
		left, right := releasePriority(pending[i].identifier.Scheme), releasePriority(pending[j].identifier.Scheme)
		if left != right {
			return left < right
		}
		return pending[i].hint.Title < pending[j].hint.Title
	})
	seen := map[string]bool{}
	result := []artistReleaseEvidence{}
	lookups := 0
	for _, lookup := range pending {
		hint, identifier := lookup.hint, lookup.identifier
		key := identifier.Scheme + "\x00" + identifier.Value
		if seen[key] || lookups >= 12 {
			continue
		}
		var evidence artistReleaseEvidence
		var matched bool
		var lookupErr error
		switch identifier.Scheme {
		case "musicbrainz":
			lookupErr = loadMusicBrainz()
			if lookupErr == nil {
				evidence, matched, lookupErr = artistReleaseEvidenceFromMusicBrainz(ctx, musicBrainzClient, hint, identifier)
			}
		case "apple":
			lookupErr = loadApple()
			if lookupErr == nil {
				evidence, matched, lookupErr = artistReleaseEvidenceFromApple(ctx, appleClient, hint, identifier)
			}
		case "deezer":
			lookupErr = loadDeezer()
			if lookupErr == nil {
				evidence, matched, lookupErr = artistReleaseEvidenceFromDeezer(ctx, deezerClient, hint, identifier)
			}
		case "discogs_release", "discogs_master":
			lookupErr = loadDiscogs()
			if lookupErr == nil {
				evidence, matched, lookupErr = artistReleaseEvidenceFromDiscogs(ctx, discogsClient, hint, identifier)
			}
		default:
			continue
		}
		seen[key] = true
		lookups++
		if lookupErr != nil {
			return nil, lookupErr
		}
		if matched {
			result = append(result, evidence)
		}
	}
	return result, nil
}

func artistReleaseEvidenceFromApple(ctx context.Context, client *apple.Client, hint ReleaseHint, identifier Identifier) (artistReleaseEvidence, bool, error) {
	payloads, err := client.Collect(ctx, providers.Identifier{Provider: "apple", Namespace: "album", Value: identifier.Value})
	if err != nil {
		return artistReleaseEvidence{}, false, err
	}
	if len(payloads) == 0 || payloads[0].StatusCode == http.StatusNotFound {
		return artistReleaseEvidence{}, false, nil
	}
	if payloads[0].StatusCode != http.StatusOK {
		return artistReleaseEvidence{}, false, &providers.StatusError{Provider: "apple", StatusCode: payloads[0].StatusCode}
	}
	var envelope struct {
		ResultCount int `json:"resultCount"`
	}
	if err := json.Unmarshal(payloads[0].Body, &envelope); err == nil && envelope.ResultCount == 0 {
		return artistReleaseEvidence{}, false, nil
	}
	record, err := apple.NormalizeAlbum(payloads[0].Body, identifier.Value, "", time.Now().UTC())
	if err != nil {
		return artistReleaseEvidence{}, false, err
	}
	return artistReleaseEvidenceFromNormalizedRecord(hint, identifier, record)
}

func artistReleaseEvidenceFromDeezer(ctx context.Context, client *deezer.Client, hint ReleaseHint, identifier Identifier) (artistReleaseEvidence, bool, error) {
	payloads, err := client.Collect(ctx, providers.Identifier{Provider: "deezer", Namespace: "album", Value: identifier.Value})
	if err != nil {
		return artistReleaseEvidence{}, false, err
	}
	if len(payloads) == 0 || payloads[0].StatusCode == http.StatusNotFound {
		return artistReleaseEvidence{}, false, nil
	}
	if payloads[0].StatusCode != http.StatusOK {
		return artistReleaseEvidence{}, false, &providers.StatusError{Provider: "deezer", StatusCode: payloads[0].StatusCode}
	}
	var envelope struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payloads[0].Body, &envelope); err == nil && envelope.Error != nil {
		if envelope.Error.Code == 800 || strings.Contains(strings.ToLower(envelope.Error.Message), "not found") {
			return artistReleaseEvidence{}, false, nil
		}
		return artistReleaseEvidence{}, false, fmt.Errorf("Deezer album lookup: %s", envelope.Error.Message)
	}
	record, err := deezer.NormalizeAlbum(payloads[0].Body, "", time.Now().UTC())
	if err != nil {
		return artistReleaseEvidence{}, false, err
	}
	return artistReleaseEvidenceFromNormalizedRecord(hint, identifier, record)
}

func artistReleaseEvidenceFromDiscogs(ctx context.Context, client *discogs.Client, hint ReleaseHint, identifier Identifier) (artistReleaseEvidence, bool, error) {
	namespace := strings.TrimPrefix(identifier.Scheme, "discogs_")
	payloads, err := client.Collect(ctx, providers.Identifier{Provider: "discogs", Namespace: namespace, Value: identifier.Value})
	if err != nil {
		return artistReleaseEvidence{}, false, err
	}
	if len(payloads) == 0 || payloads[0].StatusCode == http.StatusNotFound {
		return artistReleaseEvidence{}, false, nil
	}
	if payloads[0].StatusCode != http.StatusOK {
		return artistReleaseEvidence{}, false, &providers.StatusError{Provider: "discogs", StatusCode: payloads[0].StatusCode}
	}
	var record rgdomain.NormalizedRecordV1
	if namespace == "master" {
		record, err = discogs.NormalizeMaster(payloads[0].Body, "", time.Now().UTC())
	} else {
		record, err = discogs.NormalizeRelease(payloads[0].Body, "", time.Now().UTC())
	}
	if err != nil {
		return artistReleaseEvidence{}, false, err
	}
	return artistReleaseEvidenceFromNormalizedRecord(hint, identifier, record)
}

func artistReleaseEvidenceFromNormalizedRecord(hint ReleaseHint, identifier Identifier, record rgdomain.NormalizedRecordV1) (artistReleaseEvidence, bool, error) {
	title := ""
	for _, value := range record.Titles {
		if title == "" || value.Primary {
			title = value.Value
		}
		if value.Primary {
			break
		}
	}
	date := ""
	if len(record.Dates) > 0 {
		date = record.Dates[0].Value
	}
	primaryType := record.Classification.PrimaryType
	// iTunes labels every collection wrapper as an album, and Discogs does not
	// expose a dependable album/single/EP classification on these resources.
	// Exact title and date still make their explicit IDs strong evidence.
	if record.ProviderRecord.Provider == "apple" || record.ProviderRecord.Provider == "discogs" {
		primaryType = ""
	}
	if !releaseHintGroupMatches(hint, hint.Title, false, title, date, primaryType) {
		return artistReleaseEvidence{}, false, nil
	}
	result := artistReleaseEvidence{Hint: hint, Identifier: identifier}
	for _, credit := range record.ArtistCredits {
		provider := strings.ToLower(strings.TrimSpace(credit.ArtistProvider))
		value := strings.TrimSpace(credit.ArtistID)
		if value == "" || (provider != "apple" && provider != "deezer" && provider != "discogs") {
			continue
		}
		result.Credits = append(result.Credits, artistReleaseCredit{
			Root:  ingestionRoot{Kind: KindArtist, Provider: provider, Namespace: "artist", Value: value},
			Names: cleanSorted([]string{credit.Name, credit.ArtistName}),
			Role:  normalizeType(credit.Role),
		})
	}
	return result, len(result.Credits) > 0, nil
}

func artistReleaseEvidenceFromMusicBrainz(ctx context.Context, client *musicbrainz.Client, hint ReleaseHint, identifier Identifier) (artistReleaseEvidence, bool, error) {
	type creditEnvelope struct {
		Title            string `json:"title"`
		Date             string `json:"date"`
		FirstReleaseDate string `json:"first-release-date"`
		PrimaryType      string `json:"primary-type"`
		ReleaseGroup     *struct {
			PrimaryType string `json:"primary-type"`
		} `json:"release-group"`
		ArtistCredit []struct {
			Name       string `json:"name"`
			JoinPhrase string `json:"joinphrase"`
			Artist     struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				SortName string `json:"sort-name"`
				Aliases  []struct {
					Name string `json:"name"`
				} `json:"aliases"`
			} `json:"artist"`
		} `json:"artist-credit"`
	}
	for _, namespace := range []string{"release_group", "release"} {
		payloads, err := client.Collect(ctx, providers.Identifier{Provider: "musicbrainz", Namespace: namespace, Value: identifier.Value})
		if err != nil {
			return artistReleaseEvidence{}, false, err
		}
		if len(payloads) == 0 {
			continue
		}
		payload := payloads[0]
		if payload.StatusCode == http.StatusNotFound {
			continue
		}
		if payload.StatusCode != http.StatusOK {
			return artistReleaseEvidence{}, false, &providers.StatusError{Provider: "musicbrainz", StatusCode: payload.StatusCode}
		}
		var source creditEnvelope
		if err := json.Unmarshal(payload.Body, &source); err != nil {
			return artistReleaseEvidence{}, false, fmt.Errorf("decode MusicBrainz %s artist routing evidence: %w", namespace, err)
		}
		date, primaryType := source.FirstReleaseDate, source.PrimaryType
		if namespace == "release" {
			date = source.Date
			if source.ReleaseGroup != nil {
				primaryType = source.ReleaseGroup.PrimaryType
			}
		}
		if !releaseHintGroupMatches(hint, hint.Title, false, source.Title, date, primaryType) {
			return artistReleaseEvidence{}, false, nil
		}
		result := artistReleaseEvidence{Hint: hint, Identifier: identifier}
		for _, credit := range source.ArtistCredit {
			if value := strings.ToLower(strings.TrimSpace(credit.Artist.ID)); value != "" {
				names := []string{credit.Name, credit.Artist.Name, credit.Artist.SortName}
				for _, alias := range credit.Artist.Aliases {
					names = append(names, alias.Name)
				}
				result.Credits = append(result.Credits, artistReleaseCredit{
					Root:  ingestionRoot{Kind: KindArtist, Provider: "musicbrainz", Namespace: "artist", Value: value},
					Names: cleanSorted(names),
				})
			}
		}
		return result, len(result.Credits) > 0, nil
	}
	return artistReleaseEvidence{}, false, nil
}

func selectUnanchoredArtistReleaseRoots(request Request, evidence []artistReleaseEvidence) map[string]ingestionRoot {
	result := map[string]ingestionRoot{}
	targetNames := artistRequestNames(request)
	if len(targetNames) > 0 {
		for _, release := range evidence {
			for _, credit := range release.Credits {
				if artistReleaseCreditEligible(credit) && artistReleaseCreditMatchesNames(credit, targetNames) {
					result[credit.Root.key()] = credit.Root
				}
			}
		}
		return result
	}

	// A release-only request can identify an artist without a textual query
	// only when every fetched release has one billing identity. Collaborative
	// releases remain ambiguous and must not manufacture a conflict.
	for _, release := range evidence {
		eligible := eligibleArtistReleaseCredits(release.Credits)
		if len(eligible) != 1 {
			return map[string]ingestionRoot{}
		}
		result[eligible[0].Root.key()] = eligible[0].Root
	}
	return result
}

func (s *Service) selectAnchoredArtistReleaseRoots(ctx context.Context, request Request, evidence []artistReleaseEvidence, anchorRootKeys map[string]bool, anchorEntityID string) (map[string]ingestionRoot, bool, error) {
	result := map[string]ingestionRoot{}
	targetNames := artistRequestNames(request)
	canonicalNames, err := s.canonicalArtistNames(ctx, anchorEntityID)
	if err != nil {
		return nil, false, err
	}
	targetNames = cleanSorted(append(targetNames, canonicalNames...))
	corroborated := false
	for _, release := range evidence {
		eligible := eligibleArtistReleaseCredits(release.Credits)
		if len(eligible) == 0 {
			continue
		}
		matches := map[string]ingestionRoot{}
		for _, credit := range eligible {
			if anchorRootKeys[credit.Root.key()] {
				matches[credit.Root.key()] = credit.Root
			}
		}
		if len(matches) == 0 {
			for _, credit := range eligible {
				if artistReleaseCreditMatchesNames(credit, targetNames) {
					matches[credit.Root.key()] = credit.Root
				}
			}
		}
		if len(matches) == 0 {
			for _, credit := range eligible {
				entityID, claimErr := s.resolveIngestionRootClaim(ctx, credit.Root)
				switch {
				case claimErr == nil && entityID == anchorEntityID:
					matches[credit.Root.key()] = credit.Root
				case claimErr == nil, errors.Is(claimErr, pgx.ErrNoRows):
				default:
					return nil, false, claimErr
				}
			}
		}
		if len(matches) > 0 {
			corroborated = true
			for key, root := range matches {
				result[key] = root
			}
			continue
		}
		// The exact release identifier matched title/date/type but none of its
		// billing artists can be the anchored artist. This is genuine conflicting
		// evidence (for example, a stale artist ID paired with another act's solo
		// release), not an ordinary collaboration.
		for _, credit := range eligible {
			result[credit.Root.key()] = credit.Root
		}
	}
	return result, corroborated, nil
}

func (s *Service) canonicalArtistNames(ctx context.Context, entityID string) ([]string, error) {
	if strings.TrimSpace(entityID) == "" {
		return nil, nil
	}
	rows, err := s.runtime.DB.Query(ctx, `SELECT value FROM search_names WHERE entity_id=$1 ORDER BY value`, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return cleanSorted(result), rows.Err()
}

func artistRequestNames(request Request) []string {
	return cleanSorted(append([]string{request.Query}, request.Hints.Aliases...))
}

func eligibleArtistReleaseCredits(values []artistReleaseCredit) []artistReleaseCredit {
	result := make([]artistReleaseCredit, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if !artistReleaseCreditEligible(value) || seen[value.Root.key()] {
			continue
		}
		seen[value.Root.key()] = true
		result = append(result, value)
	}
	return result
}

func artistReleaseCreditEligible(credit artistReleaseCredit) bool {
	role := normalizeType(credit.Role)
	switch role {
	case "", "artist", "main", "primary", "featured", "featuring":
	default:
		return false
	}
	if credit.Root.Provider == "musicbrainz" && credit.Root.Value == "89ad4ac3-39f7-470e-963a-56509c546377" {
		return false
	}
	for _, name := range credit.Names {
		if normalizedText(name) != "variousartists" {
			return true
		}
	}
	return false
}

func artistReleaseCreditMatchesNames(credit artistReleaseCredit, targetNames []string) bool {
	equivalent := func(left, right string) bool { return normalizedText(left) == normalizedText(right) }
	for _, name := range credit.Names {
		if musiccredits.ContainsName(name, targetNames, equivalent) {
			return true
		}
	}
	return false
}

func canonicalConflictCandidate(kind, entityID string, display Display) Candidate {
	return Candidate{
		Confidence: 1,
		Match:      "conflict",
		Display:    display,
		Evidence:   []Evidence{{Field: "identifiers", Outcome: "conflict", Weight: 1, Detail: "provided identifiers disagree"}},
		Resolution: Resolution{Kind: kind, Provider: "heya", Namespace: "entity", Value: entityID},
	}
}

func (s *Service) rootsForIdentifier(ctx context.Context, kind string, identifier Identifier, jobID int64, credentials providercredentials.Credentials) ([]ingestionRoot, error) {
	if root, ok := directIngestionRoot(kind, identifier); ok {
		return []ingestionRoot{root}, nil
	}
	switch {
	case kind == KindMovie && identifier.Scheme == "imdb":
		return s.movieRootsByIMDb(ctx, identifier.Value, jobID, credentials.APIKey("tmdb"))
	case kind == KindTVShow && identifier.Scheme == "tmdb":
		return s.tmdbTVRootByID(ctx, kind, identifier.Value, jobID, credentials.APIKey("tmdb"))
	case kind == KindTVShow && (identifier.Scheme == "imdb" || identifier.Scheme == "tvdb"):
		return s.preferredTVRootsByExternal(ctx, identifier, jobID, credentials.APIKey("tmdb"))
	case kind == KindTVShow && identifier.Scheme == "tvmaze":
		return s.preferredTVRootsByTVMaze(ctx, kind, identifier.Value, jobID, credentials.APIKey("tmdb"))
	case kind == KindAnime && identifier.Scheme == "tmdb":
		return s.tmdbTVRootByID(ctx, kind, identifier.Value, jobID, credentials.APIKey("tmdb"))
	case kind == KindAnime && identifier.Scheme == "anidb":
		return s.preferredAnimeRootsByAniDB(ctx, identifier.Value, jobID, credentials.APIKey("tmdb"))
	case kind == KindAnime && (identifier.Scheme == "myanimelist" || identifier.Scheme == "anilist" || identifier.Scheme == "tvdb"):
		return s.preferredAnimeRootsByExternal(ctx, identifier, jobID, credentials.APIKey("tmdb"))
	case kind == KindAnime && identifier.Scheme == "imdb":
		return s.tmdbTVRootsByExternal(ctx, kind, identifier, jobID, credentials.APIKey("tmdb"))
	case kind == KindAnime && identifier.Scheme == "tvmaze":
		return s.preferredTVRootsByTVMaze(ctx, kind, identifier.Value, jobID, credentials.APIKey("tmdb"))
	case (kind == KindBookWork || kind == KindMangaVolume || kind == KindComicVolume) && identifier.Scheme == "isbn":
		return s.bookRootsByISBN(ctx, kind, identifier.Value, jobID)
	}
	return nil, nil
}

func directIngestionRoot(kind string, identifier Identifier) (ingestionRoot, bool) {
	if !ValidIdentifierValue(identifier) {
		return ingestionRoot{}, false
	}
	root := ingestionRoot{Kind: kind, Value: identifier.Value}
	switch {
	case kind == KindMovie && identifier.Scheme == "tmdb":
		root.Provider, root.Namespace = "tmdb", "movie"
	case kind == KindArtist && identifier.Scheme == "musicbrainz":
		root.Provider, root.Namespace = "musicbrainz", "artist"
	case kind == KindArtist && identifier.Scheme == "apple":
		root.Provider, root.Namespace = "apple", "artist"
	case kind == KindArtist && identifier.Scheme == "deezer":
		root.Provider, root.Namespace = "deezer", "artist"
	case kind == KindReleaseGroup && identifier.Scheme == "musicbrainz":
		root.Provider, root.Namespace = "musicbrainz", "release_group"
	case kind == KindRecording && identifier.Scheme == "musicbrainz":
		root.Provider, root.Namespace = "musicbrainz", "recording"
	case kind == KindMusicalWork && identifier.Scheme == "openopus":
		root.Provider, root.Namespace = "openopus", "work"
	case kind == KindManga && identifier.Scheme == "kitsu":
		root.Provider, root.Namespace = "kitsu", "manga"
	case (kind == KindBookWork || kind == KindMangaVolume || kind == KindComicVolume) && identifier.Scheme == "openlibrary" && strings.HasSuffix(strings.ToUpper(identifier.Value), "W"):
		root.Provider, root.Namespace = "openlibrary", "work"
	default:
		return ingestionRoot{}, false
	}
	return root, true
}

func (s *Service) movieRootsByIMDb(ctx context.Context, imdbID string, jobID int64, apiKey string) ([]ingestionRoot, error) {
	base := tmdb.New(s.runtime.Config.Providers.TMDB)
	resolver, err := providercache.New(s.runtime, "tmdb-external-routing/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return nil, err
	}
	payload, err := tmdb.NewCached(s.runtime.Config.Providers.TMDB, resolver, apiKey).FindByIMDb(ctx, imdbID)
	if err != nil {
		return nil, err
	}
	if payload.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if payload.StatusCode != http.StatusOK {
		return nil, &providers.StatusError{Provider: "tmdb", StatusCode: payload.StatusCode}
	}
	var envelope struct {
		Movies []struct {
			ID int64 `json:"id"`
		} `json:"movie_results"`
	}
	if err := json.Unmarshal(payload.Body, &envelope); err != nil {
		return nil, fmt.Errorf("decode TMDB external movie lookup: %w", err)
	}
	result := []ingestionRoot{}
	for _, movie := range envelope.Movies {
		if movie.ID > 0 {
			result = append(result, ingestionRoot{Kind: KindMovie, Provider: "tmdb", Namespace: "movie", Value: strconv.FormatInt(movie.ID, 10)})
		}
	}
	return uniqueRoots(result), nil
}

func (s *Service) preferredTVRootsByExternal(ctx context.Context, identifier Identifier, jobID int64, apiKey string) ([]ingestionRoot, error) {
	roots, err := s.tmdbTVRootsByExternal(ctx, KindTVShow, identifier, jobID, apiKey)
	if err != nil || len(roots) > 0 {
		return roots, err
	}
	return s.tvMazeRootsByExternal(ctx, KindTVShow, identifier, jobID)
}

func (s *Service) tmdbTVRootsByExternal(ctx context.Context, kind string, identifier Identifier, jobID int64, apiKey string) ([]ingestionRoot, error) {
	base := tmdb.New(s.runtime.Config.Providers.TMDB)
	resolver, err := providercache.New(s.runtime, "tmdb-tv-external-routing/v2", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return nil, err
	}
	payload, err := tmdb.NewCached(s.runtime.Config.Providers.TMDB, resolver, apiKey).FindTVByExternal(ctx, identifier.Scheme, identifier.Value)
	if err != nil {
		return nil, err
	}
	if payload.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if payload.StatusCode != http.StatusOK {
		return nil, &providers.StatusError{Provider: "tmdb", StatusCode: payload.StatusCode}
	}
	var envelope struct {
		TV []struct {
			ID int64 `json:"id"`
		} `json:"tv_results"`
	}
	if err := json.Unmarshal(payload.Body, &envelope); err != nil {
		return nil, fmt.Errorf("decode TMDB external TV lookup: %w", err)
	}
	result := []ingestionRoot{}
	for _, show := range envelope.TV {
		if show.ID > 0 {
			result = append(result, ingestionRoot{Kind: kind, Provider: "tmdb", Namespace: "tv", Value: strconv.FormatInt(show.ID, 10)})
		}
	}
	return uniqueRoots(result), nil
}

func (s *Service) tmdbTVRootByID(ctx context.Context, kind, id string, jobID int64, apiKey string) ([]ingestionRoot, error) {
	base := tmdb.New(s.runtime.Config.Providers.TMDB)
	resolver, err := providercache.New(s.runtime, "tmdb-tv-root-routing/v2", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return nil, err
	}
	payload, err := tmdb.NewCached(s.runtime.Config.Providers.TMDB, resolver, apiKey).TVExternalIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	if payload.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if payload.StatusCode != http.StatusOK {
		return nil, &providers.StatusError{Provider: "tmdb", StatusCode: payload.StatusCode}
	}
	return []ingestionRoot{{Kind: kind, Provider: "tmdb", Namespace: "tv", Value: strings.TrimSpace(id)}}, nil
}

func (s *Service) tvMazeRootsByExternal(ctx context.Context, kind string, identifier Identifier, jobID int64) ([]ingestionRoot, error) {
	base := tvmaze.New(s.runtime.Config.Providers.TVMaze)
	resolver, err := providercache.New(s.runtime, "tvmaze-external-routing/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return nil, err
	}
	namespace := map[string]string{"imdb": "title", "tvdb": "series"}[identifier.Scheme]
	payloads, err := tvmaze.NewCached(s.runtime.Config.Providers.TVMaze, resolver).Collect(ctx, providers.Identifier{Provider: identifier.Scheme, Namespace: namespace, Value: identifier.Value})
	if err != nil {
		return nil, err
	}
	for index := len(payloads) - 1; index >= 0; index-- {
		payload := payloads[index]
		if payload.StatusCode == http.StatusNotFound {
			continue
		}
		if payload.StatusCode != http.StatusOK {
			return nil, &providers.StatusError{Provider: "tvmaze", StatusCode: payload.StatusCode}
		}
		var show struct {
			ID int64 `json:"id"`
		}
		if json.Unmarshal(payload.Body, &show) == nil && show.ID > 0 {
			return []ingestionRoot{{Kind: kind, Provider: "tvmaze", Namespace: "show", Value: strconv.FormatInt(show.ID, 10)}}, nil
		}
	}
	return nil, nil
}

func (s *Service) preferredTVRootsByTVMaze(ctx context.Context, kind, id string, jobID int64, apiKey string) ([]ingestionRoot, error) {
	base := tvmaze.New(s.runtime.Config.Providers.TVMaze)
	resolver, err := providercache.New(s.runtime, "tvmaze-root-routing/v2", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return nil, err
	}
	payloads, err := tvmaze.NewCached(s.runtime.Config.Providers.TVMaze, resolver).Collect(ctx, providers.Identifier{Provider: "tvmaze", Namespace: "show", Value: id})
	if err != nil {
		return nil, err
	}
	if len(payloads) == 0 || payloads[len(payloads)-1].StatusCode == http.StatusNotFound {
		return nil, nil
	}
	payload := payloads[len(payloads)-1]
	if payload.StatusCode != http.StatusOK {
		return nil, &providers.StatusError{Provider: "tvmaze", StatusCode: payload.StatusCode}
	}
	var show struct {
		ID        int64 `json:"id"`
		Externals struct {
			TVDB int64  `json:"thetvdb"`
			IMDb string `json:"imdb"`
		} `json:"externals"`
	}
	if err := json.Unmarshal(payload.Body, &show); err != nil {
		return nil, fmt.Errorf("decode TVMaze routing detail: %w", err)
	}
	for _, identifier := range []Identifier{{Scheme: "tvdb", Value: strconv.FormatInt(show.Externals.TVDB, 10)}, {Scheme: "imdb", Value: show.Externals.IMDb}} {
		if identifier.Value == "" || identifier.Value == "0" {
			continue
		}
		roots, lookupErr := s.tmdbTVRootsByExternal(ctx, kind, identifier, jobID, apiKey)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if len(roots) > 0 {
			return roots, nil
		}
	}
	if show.ID < 1 {
		return nil, nil
	}
	return []ingestionRoot{{Kind: kind, Provider: "tvmaze", Namespace: "show", Value: strconv.FormatInt(show.ID, 10)}}, nil
}

func (s *Service) preferredAnimeRootsByExternal(ctx context.Context, identifier Identifier, jobID int64, apiKey string) ([]ingestionRoot, error) {
	base := animelists.New(s.runtime.Config.Providers.AnimeLists)
	resolver, err := providercache.New(s.runtime, "anime-lists-reverse-routing/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return nil, err
	}
	payload, entry, ok, err := animelists.NewCached(s.runtime.Config.Providers.AnimeLists, resolver).LookupExternal(ctx, identifier.Scheme, identifier.Value)
	if err != nil {
		return nil, err
	}
	if payload.StatusCode != http.StatusOK {
		return nil, &providers.StatusError{Provider: "anime_lists", StatusCode: payload.StatusCode}
	}
	if !ok || entry.AniDBID < 1 {
		return nil, nil
	}
	return s.preferredAnimeRootFromMapping(ctx, entry, jobID, apiKey)
}

func (s *Service) preferredAnimeRootsByAniDB(ctx context.Context, aid string, jobID int64, apiKey string) ([]ingestionRoot, error) {
	base := animelists.New(s.runtime.Config.Providers.AnimeLists)
	resolver, err := providercache.New(s.runtime, "anime-lists-anidb-routing/v2", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return nil, err
	}
	payload, entry, ok, err := animelists.NewCached(s.runtime.Config.Providers.AnimeLists, resolver).Lookup(ctx, aid)
	if err != nil {
		return nil, err
	}
	if payload.StatusCode != http.StatusOK {
		return nil, &providers.StatusError{Provider: "anime_lists", StatusCode: payload.StatusCode}
	}
	if !ok {
		return []ingestionRoot{{Kind: KindAnime, Provider: "anidb", Namespace: "anime", Value: strings.TrimSpace(aid)}}, nil
	}
	return s.preferredAnimeRootFromMapping(ctx, entry, jobID, apiKey)
}

func (s *Service) preferredAnimeRootFromMapping(ctx context.Context, entry animelists.Entry, jobID int64, apiKey string) ([]ingestionRoot, error) {
	if entry.TMDBID.TV > 0 {
		roots, err := s.tmdbTVRootByID(ctx, KindAnime, strconv.Itoa(entry.TMDBID.TV), jobID, apiKey)
		if err != nil || len(roots) > 0 {
			return roots, err
		}
	}
	if entry.TVDBID > 0 {
		roots, err := s.tmdbTVRootsByExternal(ctx, KindAnime, Identifier{Scheme: "tvdb", Value: strconv.Itoa(entry.TVDBID)}, jobID, apiKey)
		if err != nil || len(roots) > 0 {
			return roots, err
		}
	}
	if entry.AniDBID > 0 {
		return []ingestionRoot{{Kind: KindAnime, Provider: "anidb", Namespace: "anime", Value: strconv.Itoa(entry.AniDBID)}}, nil
	}
	return nil, nil
}

func (s *Service) bookRootsByISBN(ctx context.Context, kind, isbn string, jobID int64) ([]ingestionRoot, error) {
	base := openlibrary.New(s.runtime.Config.Providers.OpenLibrary)
	resolver, err := providercache.New(s.runtime, "openlibrary-isbn-routing/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return nil, err
	}
	payload, err := openlibrary.NewCached(s.runtime.Config.Providers.OpenLibrary, resolver).LookupISBN(ctx, isbn)
	if err != nil {
		return nil, err
	}
	if payload.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if payload.StatusCode != http.StatusOK {
		return nil, &providers.StatusError{Provider: "openlibrary", StatusCode: payload.StatusCode}
	}
	var envelope struct {
		Works []struct {
			Key string `json:"key"`
		} `json:"works"`
	}
	if err := json.Unmarshal(payload.Body, &envelope); err != nil {
		return nil, fmt.Errorf("decode Open Library ISBN lookup: %w", err)
	}
	result := []ingestionRoot{}
	for _, work := range envelope.Works {
		key := strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(work.Key), "/works/"))
		if strings.HasPrefix(key, "OL") && strings.HasSuffix(key, "W") {
			result = append(result, ingestionRoot{Kind: kind, Provider: "openlibrary", Namespace: "work", Value: key})
		}
	}
	return uniqueRoots(result), nil
}

func (s *Service) ingestRoot(ctx context.Context, root ingestionRoot, jobID int64, credentials providercredentials.Credentials) (string, error) {
	switch root.Kind {
	case KindMovie:
		id, err := strconv.ParseInt(root.Value, 10, 64)
		if err != nil || id < 1 {
			return "", fmt.Errorf("invalid internal movie ingestion identity")
		}
		result, err := movies.NewService(s.runtime).IngestTMDBWithCredentials(ctx, id, jobID, credentials)
		return result.EntityID, err
	case KindTVShow:
		service := tvshows.NewService(s.runtime)
		var result episodic.Result
		var err error
		switch root.Provider {
		case "tmdb":
			result, err = service.IngestTMDBWithCredentials(ctx, root.Value, jobID, credentials)
		case "tvmaze":
			result, err = service.IngestTVMazeWithCredentials(ctx, root.Value, jobID, credentials)
		default:
			return "", fmt.Errorf("no internal TV ingestion route for %q", root.Provider)
		}
		return result.EntityID, err
	case KindAnime:
		service := anime.NewService(s.runtime)
		var result episodic.Result
		var err error
		switch root.Provider {
		case "tmdb":
			result, err = service.IngestTMDBWithCredentials(ctx, root.Value, jobID, credentials)
		case "anidb":
			result, err = service.IngestAniDBWithCredentials(ctx, root.Value, jobID, credentials)
		case "tvmaze":
			result, err = service.IngestTVMazeWithCredentials(ctx, root.Value, jobID, credentials)
		default:
			return "", fmt.Errorf("no internal anime ingestion route for %q", root.Provider)
		}
		return result.EntityID, err
	case KindArtist:
		service := artists.NewService(s.runtime)
		var result artists.Result
		var err error
		switch root.Provider {
		case "musicbrainz":
			result, err = service.IngestMusicBrainz(ctx, root.Value, jobID, credentials)
		case "apple":
			result, err = service.IngestApple(ctx, root.Value, jobID, credentials)
		case "deezer":
			result, err = service.IngestDeezer(ctx, root.Value, jobID, credentials)
		default:
			return "", fmt.Errorf("no internal artist ingestion route for %q", root.Provider)
		}
		return result.EntityID, err
	case KindReleaseGroup:
		result, err := releasegroups.NewService(s.runtime).IngestMusicBrainz(ctx, root.Value, jobID, credentials)
		return result.EntityID, err
	case KindRecording:
		artistService := artists.NewService(s.runtime)
		materialize := recordings.WithArtistCreditMaterializer(func(ctx context.Context, mbid string) error {
			_, err := artistService.EnsureMusicBrainzIdentity(ctx, mbid)
			return err
		})
		result, err := recordings.NewService(s.runtime, materialize).IngestMusicBrainz(ctx, root.Value, jobID)
		return result.EntityID, err
	case KindMusicalWork:
		result, err := musicalworks.NewService(s.runtime).IngestOpenOpus(ctx, root.Value, jobID)
		return result.EntityID, err
	case KindManga:
		result, err := manga.NewService(s.runtime).IngestKitsu(ctx, root.Value, jobID, credentials)
		return result.ID, err
	case KindBookWork, KindMangaVolume, KindComicVolume:
		result, err := books.NewService(s.runtime).IngestWorkAs(ctx, root.Value, root.Kind, jobID, credentials)
		return result.ID, err
	default:
		return "", fmt.Errorf("no internal ingestion route for %q", root.Kind)
	}
}

func uniqueRoots(values []ingestionRoot) []ingestionRoot {
	seen := map[string]bool{}
	result := make([]ingestionRoot, 0, len(values))
	for _, value := range values {
		if !seen[value.key()] {
			seen[value.key()] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].key() < result[j].key() })
	return result
}
