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
	"github.com/HeyaMedia/HeyaMetadata/internal/episodic"
	"github.com/HeyaMedia/HeyaMetadata/internal/manga"
	"github.com/HeyaMedia/HeyaMetadata/internal/movies"
	"github.com/HeyaMedia/HeyaMetadata/internal/musicalworks"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/animelists"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/openlibrary"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tmdb"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tvmaze"
	"github.com/HeyaMedia/HeyaMetadata/internal/recordings"
	"github.com/HeyaMedia/HeyaMetadata/internal/releasegroups"
	"github.com/HeyaMedia/HeyaMetadata/internal/tvshows"
)

type ingestionRoot struct {
	Kind      string
	Provider  string
	Namespace string
	Value     string
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
	roots := map[string]ingestionRoot{}
	rootEvidence := map[string][]int{}
	for index, identifier := range request.Identifiers {
		// ResolveKnownIdentifiers has already classified every identifier. Only
		// fresh supported evidence needs an upstream crosswalk; re-routing a known
		// identifier can needlessly ingest a second copy of the same entity.
		if result.IdentifierEvidence[index].Outcome != "unused" {
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
			rootEvidence[key] = append(rootEvidence[key], index)
		}
	}
	if len(roots) == 0 {
		// Exact identifier evidence outranks fuzzy title discovery. If any
		// identifier already established a canonical entity, return it even when
		// another supported identifier could not be crosswalked upstream.
		if knownEntityID != "" {
			return result, true, nil
		}
		if request.Query != "" {
			return result, false, nil
		}
		return result, true, nil
	}
	if len(roots) > 1 {
		result.EntityID = ""
		result.Status = "needs_selection"
		result.Recommendation = "conflicting_identifiers"
		if knownEntityID != "" {
			for index := range result.IdentifierEvidence {
				if result.IdentifierEvidence[index].Outcome == "resolved" || result.IdentifierEvidence[index].Outcome == "corroborating" {
					result.IdentifierEvidence[index].Outcome = "conflict"
					result.IdentifierEvidence[index].Detail = "identifier resolves to a different canonical Heya entity"
				}
			}
			display, err := s.canonicalCandidateDisplay(ctx, knownEntityID)
			if err != nil {
				return Result{}, false, err
			}
			result.Candidates = append(result.Candidates, canonicalConflictCandidate(request.Kind, knownEntityID, display))
		}
		keys := make([]string, 0, len(roots))
		for key, indexes := range rootEvidence {
			keys = append(keys, key)
			for _, index := range indexes {
				result.IdentifierEvidence[index].Outcome = "conflict"
				result.IdentifierEvidence[index].Detail = "provided identifiers map to different upstream identities"
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			root := roots[key]
			result.Candidates = append(result.Candidates, Candidate{
				Confidence: 1, Match: "conflict",
				Display:    Display{Title: request.Query, Name: request.Query, Year: request.Hints.Year},
				Evidence:   []Evidence{{Field: "identifiers", Outcome: "conflict", Weight: 1, Detail: "provided identifiers disagree"}},
				Resolution: Resolution{Kind: root.Kind, Provider: root.Provider, Namespace: root.Namespace, Value: root.Value},
			})
		}
		for index := range result.Candidates {
			result.Candidates[index].Rank = index + 1
		}
		return result, true, nil
	}
	var root ingestionRoot
	for _, value := range roots {
		root = value
	}
	for _, indexes := range rootEvidence {
		for _, index := range indexes[1:] {
			result.IdentifierEvidence[index].Outcome = "corroborating"
		}
	}
	entityID, err := s.ingestRoot(ctx, root, jobID, credentials)
	if err != nil {
		var status *providers.StatusError
		if errors.As(err, &status) && status.StatusCode == http.StatusNotFound {
			for _, indexes := range rootEvidence {
				for _, index := range indexes {
					result.IdentifierEvidence[index].Outcome = "unused"
					result.IdentifierEvidence[index].Detail = "upstream identity was not found"
				}
			}
			return result, true, nil
		}
		return Result{}, false, err
	}
	if knownEntityID != "" && entityID != knownEntityID {
		result.EntityID = ""
		result.Status = "needs_selection"
		result.Recommendation = "conflicting_identifiers"
		for index := range result.IdentifierEvidence {
			if result.IdentifierEvidence[index].Outcome == "resolved" || result.IdentifierEvidence[index].Outcome == "corroborating" {
				result.IdentifierEvidence[index].Outcome = "conflict"
				result.IdentifierEvidence[index].Detail = "identifier resolves to a different canonical Heya entity"
			}
		}
		entityIDs := []string{knownEntityID, entityID}
		sort.Strings(entityIDs)
		for _, candidateEntityID := range entityIDs {
			display, displayErr := s.canonicalCandidateDisplay(ctx, candidateEntityID)
			if displayErr != nil {
				return Result{}, false, displayErr
			}
			result.Candidates = append(result.Candidates, canonicalConflictCandidate(request.Kind, candidateEntityID, display))
		}
		for index := range result.Candidates {
			result.Candidates[index].Rank = index + 1
		}
		result.ObservedAt = time.Now().UTC()
		return result, true, nil
	}
	result.EntityID = entityID
	result.Recommendation = "identified"
	if knownEntityID != "" {
		result.Recommendation = "corroborated_identity"
		for _, indexes := range rootEvidence {
			for _, index := range indexes {
				result.IdentifierEvidence[index].Outcome = "corroborating"
				result.IdentifierEvidence[index].Detail = "identifier independently converged on the same canonical Heya entity"
			}
		}
	}
	result.ObservedAt = time.Now().UTC()
	return result, true, nil
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
		result, err := recordings.NewService(s.runtime).IngestMusicBrainz(ctx, root.Value, jobID)
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
