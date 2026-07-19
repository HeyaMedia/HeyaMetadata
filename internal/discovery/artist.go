package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/HeyaMedia/HeyaMetadata/internal/musiccatalog"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/musicbrainz"
	"github.com/jackc/pgx/v5"
)

type Service struct{ runtime *platform.Runtime }

func NewService(runtime *platform.Runtime) *Service { return &Service{runtime: runtime} }

type mbArtistSearch struct {
	Artists []struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		SortName       string `json:"sort-name"`
		Disambiguation string `json:"disambiguation"`
		Type           string `json:"type"`
		Country        string `json:"country"`
		Score          int    `json:"score"`
		Area           *struct {
			Name string `json:"name"`
		} `json:"area"`
		LifeSpan struct {
			Begin string `json:"begin"`
			End   string `json:"end"`
			Ended *bool  `json:"ended"`
		} `json:"life-span"`
		Aliases []struct {
			Name string `json:"name"`
		} `json:"aliases"`
	} `json:"artists"`
}
type mbReleaseSearch struct {
	ReleaseGroups []struct {
		ID               string `json:"id"`
		Title            string `json:"title"`
		FirstReleaseDate string `json:"first-release-date"`
		PrimaryType      string `json:"primary-type"`
		ArtistCredit     []struct {
			Artist struct {
				ID string `json:"id"`
			} `json:"artist"`
		} `json:"artist-credit"`
	} `json:"release-groups"`
}

type artistReleaseMatch struct {
	HintKey string
	Artist  ExternalID
	Release ExternalID
}

func releaseHintArtistIDs(query string, source mbArtistSearch) []string {
	exact := make([]string, 0, len(source.Artists))
	fallback := make([]string, 0, min(25, len(source.Artists)))
	want := normalizedText(query)
	for _, artist := range source.Artists {
		id := strings.ToLower(strings.TrimSpace(artist.ID))
		if id == "" {
			continue
		}
		if len(fallback) < 25 {
			fallback = append(fallback, id)
		}
		matches := normalizedText(artist.Name) == want
		for _, alias := range artist.Aliases {
			matches = matches || normalizedText(alias.Name) == want
		}
		if matches {
			exact = append(exact, id)
		}
	}
	if len(exact) > 0 {
		return cleanSortedLower(exact)
	}
	return cleanSortedLower(fallback)
}

func artistReleaseSearchQuery(title string, artistIDs []string) string {
	title = escapeLucene(title)
	query := `(releasegroup:"` + title + `" OR alias:"` + title + `" OR release:"` + title + `")`
	if len(artistIDs) == 0 {
		return query
	}
	ids := make([]string, 0, len(artistIDs))
	for _, id := range artistIDs {
		ids = append(ids, "arid:"+id)
	}
	return query + " AND (" + strings.Join(ids, " OR ") + ")"
}

func releaseHintSearchTitles(title string) []string {
	titles := []string{strings.TrimSpace(title)}
	words := strings.Fields(title)
	if len(words) == 0 {
		return titles
	}
	if _, err := strconv.Atoi(strings.Trim(strings.ToLower(words[len(words)-1]), ".#")); err == nil {
		words = words[:len(words)-1]
	}
	if len(words) >= 2 && strings.EqualFold(strings.Trim(words[len(words)-1], "."), "vol") {
		words = words[:len(words)-1]
	}
	if len(words) > 0 && strings.EqualFold(strings.Trim(words[len(words)-1], "."), "ost") {
		words = words[:len(words)-1]
	} else if len(words) >= 2 && strings.EqualFold(words[len(words)-2], "original") && strings.EqualFold(words[len(words)-1], "soundtrack") {
		words = words[:len(words)-2]
	}
	fallback := strings.TrimSpace(strings.Join(words, " "))
	if fallback != "" && normalizedText(fallback) != normalizedText(title) {
		titles = append(titles, fallback)
	}
	return titles
}

func releaseHintGroupMatches(hint ReleaseHint, searchedTitle string, fallback bool, groupTitle, firstReleaseDate, primaryType string) bool {
	y := releaseYear(firstReleaseDate)
	if hint.Year > 0 && (y == 0 || abs(y-hint.Year) > 2) {
		return false
	}
	wantType, gotType := normalizeType(hint.Type), normalizeType(primaryType)
	if !fallback {
		if normalizedText(groupTitle) == normalizedText(hint.Title) {
			// Local downloaders and tags frequently label one-track singles as
			// albums. Exact title plus compatible year is stronger discovery
			// evidence than that advisory release type.
			return true
		}
		if (wantType == "album" || wantType == "single" || wantType == "ep") && gotType != "" && wantType != gotType {
			return false
		}
		// The search may have matched a release-group alias or one of its
		// issued release titles, neither of which MusicBrainz includes in
		// this compact result. Require the exact hinted year before trusting
		// that otherwise invisible evidence; Lucene phrase search can also
		// return a longer title which merely contains the requested words.
		return hint.Year > 0 && y == hint.Year
	}
	if (wantType == "album" || wantType == "single" || wantType == "ep") && gotType != "" && wantType != gotType {
		return false
	}
	group, searched := normalizedText(groupTitle), normalizedText(searchedTitle)
	return group == searched || (len([]rune(searched)) >= 4 && strings.Contains(group, searched))
}

func (s *Service) DiscoverArtist(ctx context.Context, request Request, jobID int64) (Result, error) {
	request = NormalizeRequest(request)
	if request.Kind != KindArtist {
		return Result{}, fmt.Errorf("artist discovery requires kind artist")
	}
	if request.Query == "" {
		return Result{}, fmt.Errorf("discovery query is required")
	}
	base := musicbrainz.New(s.runtime.Config.Providers.MusicBrainz)
	resolver, err := providercache.New(s.runtime, "musicbrainz-discovery/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return Result{}, err
	}
	client := musicbrainz.NewCached(s.runtime.Config.Providers.MusicBrainz, resolver)
	query := `(artist:"` + escapeLucene(request.Query) + `" OR alias:"` + escapeLucene(request.Query) + `")`
	payload, err := client.Search(ctx, "artist", query, min(100, max(25, request.Limit*4)), 0)
	if err != nil {
		return Result{}, err
	}
	if payload.StatusCode != http.StatusOK {
		return Result{}, &providers.StatusError{Provider: "musicbrainz", StatusCode: payload.StatusCode}
	}
	var source mbArtistSearch
	if err := json.Unmarshal(payload.Body, &source); err != nil {
		return Result{}, fmt.Errorf("decode MusicBrainz artist search: %w", err)
	}
	releaseMatches := map[string][]ReleaseHint{}
	releaseIdentityMatches := map[string][]artistReleaseMatch{}
	releaseArtistIDs := releaseHintArtistIDs(request.Query, source)
	for _, hint := range request.Hints.Releases {
		for searchIndex, searchTitle := range releaseHintSearchTitles(hint.Title) {
			releaseQuery := artistReleaseSearchQuery(searchTitle, releaseArtistIDs)
			releasePayload, e := client.Search(ctx, "release_group", releaseQuery, 100, 0)
			if e != nil {
				return Result{}, e
			}
			if releasePayload.StatusCode != http.StatusOK {
				return Result{}, &providers.StatusError{Provider: "musicbrainz", StatusCode: releasePayload.StatusCode}
			}
			var releases mbReleaseSearch
			if e := json.Unmarshal(releasePayload.Body, &releases); e != nil {
				return Result{}, e
			}
			matched := false
			for _, group := range releases.ReleaseGroups {
				if !releaseHintGroupMatches(hint, searchTitle, searchIndex > 0, group.Title, group.FirstReleaseDate, group.PrimaryType) {
					continue
				}
				releaseGroupID := strings.ToLower(strings.TrimSpace(group.ID))
				for _, credit := range group.ArtistCredit {
					if credit.Artist.ID != "" {
						id := strings.ToLower(credit.Artist.ID)
						releaseMatches[id] = appendUniqueReleaseHint(releaseMatches[id], hint)
						if releaseGroupID != "" {
							releaseIdentityMatches[id] = appendUniqueArtistReleaseMatch(releaseIdentityMatches[id], artistReleaseMatch{
								HintKey: releaseHintIdentityKey(hint),
								Artist:  ExternalID{Provider: "musicbrainz", Namespace: "artist", Value: id},
								Release: ExternalID{Provider: "musicbrainz", Namespace: "release_group", Value: releaseGroupID},
							})
						}
						matched = true
					}
				}
			}
			if matched {
				break
			}
		}
	}
	candidates := make([]Candidate, 0, len(source.Artists))
	for _, value := range source.Artists {
		id := strings.ToLower(value.ID)
		aliases := make([]string, 0, len(value.Aliases))
		for _, alias := range value.Aliases {
			if strings.TrimSpace(alias.Name) != "" {
				aliases = append(aliases, alias.Name)
			}
		}
		aliases = cleanSorted(aliases)
		display := Display{Name: value.Name, SortName: value.SortName, Disambiguation: value.Disambiguation, Type: normalizeType(value.Type), Country: strings.ToUpper(value.Country), BeginDate: value.LifeSpan.Begin, EndDate: value.LifeSpan.End, Ended: value.LifeSpan.Ended, Aliases: aliases}
		if value.Area != nil {
			display.Area = value.Area.Name
		}
		candidate := Candidate{ProviderScore: value.Score, Identity: ExternalID{Provider: "musicbrainz", Namespace: "artist", Value: id}, Display: display, MatchedReleases: releaseMatches[id], Resolution: Resolution{Kind: KindArtist, Provider: "musicbrainz", Namespace: "artist", Value: id}, artistReleaseMatches: releaseIdentityMatches[id]}
		scoreCandidate(request, &candidate)
		var entityID string
		e := s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='artist' AND provider='musicbrainz' AND namespace='artist' AND normalized_value=$1 AND state='accepted'`, id).Scan(&entityID)
		if e == nil {
			candidate.ExistingEntityID = entityID
		} else if e != pgx.ErrNoRows {
			return Result{}, e
		}
		candidates = append(candidates, candidate)
	}
	providersUsed := []string{"musicbrainz"}
	storefrontCandidates, storefrontProviders, storefrontErr := s.discoverStorefrontArtistCandidates(ctx, request, jobID)
	if storefrontErr != nil {
		slog.WarnContext(ctx, "supplemental artist discovery failed", "error", storefrontErr)
	} else {
		candidates = append(candidates, storefrontCandidates...)
		providersUsed = append(providersUsed, storefrontProviders...)
	}
	candidates = dedupeExistingArtistCandidates(candidates)
	candidates = filterWeakArtistCandidates(request, candidates)
	var convergence *ArtistIdentityConvergence
	candidates, convergence, err = s.consolidatePersistedArtistCandidates(ctx, request, candidates)
	if err != nil {
		return Result{}, err
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Confidence != candidates[j].Confidence {
			return candidates[i].Confidence > candidates[j].Confidence
		}
		if candidates[i].ProviderScore != candidates[j].ProviderScore {
			return candidates[i].ProviderScore > candidates[j].ProviderScore
		}
		return candidates[i].Identity.Value < candidates[j].Identity.Value
	})
	recommended, candidates := presentCandidates(candidates, request.Limit)
	result := Result{SchemaVersion: SchemaVersion, Kind: KindArtist, Query: request.Query, Status: "completed", Recommendation: recommended, Candidates: candidates, Providers: providersUsed, ObservedAt: time.Now().UTC(), ArtistConvergence: convergence}
	return result, nil
}

func NormalizeRequest(request Request) Request {
	request.Kind = strings.ToLower(strings.TrimSpace(request.Kind))
	request.Query = strings.TrimSpace(request.Query)
	identifiers := make([]Identifier, 0, len(request.Identifiers))
	seenIdentifiers := map[string]bool{}
	for _, identifier := range request.Identifiers {
		identifier = normalizeIdentifier(identifier)
		key := identifier.Scheme + "\x00" + identifier.Value
		if identifier.Scheme != "" && identifier.Value != "" && !seenIdentifiers[key] {
			seenIdentifiers[key] = true
			identifiers = append(identifiers, identifier)
		}
	}
	sort.Slice(identifiers, func(i, j int) bool {
		if identifiers[i].Scheme != identifiers[j].Scheme {
			return identifiers[i].Scheme < identifiers[j].Scheme
		}
		return identifiers[i].Value < identifiers[j].Value
	})
	request.Identifiers = identifiers
	if request.Limit < 1 || request.Limit > 25 {
		request.Limit = 10
	}
	request.Hints.Country = strings.ToUpper(strings.TrimSpace(request.Hints.Country))
	request.Hints.Language = strings.ToLower(strings.TrimSpace(request.Hints.Language))
	request.Hints.Area = strings.TrimSpace(request.Hints.Area)
	request.Hints.Type = normalizeType(request.Hints.Type)
	request.Hints.Date = strings.TrimSpace(request.Hints.Date)
	request.Hints.OriginalTitle = strings.TrimSpace(request.Hints.OriginalTitle)
	request.Hints.BeginDate = strings.TrimSpace(request.Hints.BeginDate)
	request.Hints.EndDate = strings.TrimSpace(request.Hints.EndDate)
	request.Hints.Aliases = cleanSorted(request.Hints.Aliases)
	request.Hints.Artists = cleanSorted(request.Hints.Artists)
	request.Hints.ArtistIDs = cleanSortedLower(request.Hints.ArtistIDs)
	request.Hints.Composers = cleanSorted(request.Hints.Composers)
	request.Hints.ComposerIDs = cleanSortedLower(request.Hints.ComposerIDs)
	request.Hints.Catalogue = strings.TrimSpace(request.Hints.Catalogue)
	request.Hints.Tracks = cleanSorted(request.Hints.Tracks)
	request.Hints.ISRCs = cleanSortedUpper(request.Hints.ISRCs)
	request.Hints.Network = strings.TrimSpace(request.Hints.Network)
	request.Hints.Status = normalizeType(request.Hints.Status)
	request.Hints.Season = normalizeType(request.Hints.Season)
	request.Hints.Source = normalizeType(request.Hints.Source)
	request.Hints.Studios = cleanSorted(request.Hints.Studios)
	episodes := make([]EpisodeHint, 0, len(request.Hints.Episodes))
	seenEpisodes := map[string]bool{}
	for _, hint := range request.Hints.Episodes {
		hint.Title = strings.TrimSpace(hint.Title)
		key := normalizedText(hint.Title) + ":" + strconv.Itoa(hint.Season) + ":" + strconv.Itoa(hint.Number)
		if key != "::0" && !seenEpisodes[key] {
			seenEpisodes[key] = true
			episodes = append(episodes, hint)
		}
	}
	sort.Slice(episodes, func(i, j int) bool {
		if episodes[i].Season != episodes[j].Season {
			return episodes[i].Season < episodes[j].Season
		}
		if episodes[i].Number != episodes[j].Number {
			return episodes[i].Number < episodes[j].Number
		}
		return episodes[i].Title < episodes[j].Title
	})
	request.Hints.Episodes = episodes
	releases := make([]ReleaseHint, 0, len(request.Hints.Releases))
	releaseIndexes := map[string]int{}
	for _, hint := range request.Hints.Releases {
		hint.Title = strings.TrimSpace(hint.Title)
		hint.Type = normalizeType(hint.Type)
		hint.Identifiers = normalizeReleaseIdentifiers(hint.Identifiers)
		key := normalizedText(hint.Title) + ":" + strconv.Itoa(hint.Year) + ":" + hint.Type
		if hint.Title == "" {
			continue
		}
		if index, exists := releaseIndexes[key]; exists {
			releases[index].Identifiers = mergeIdentifiers(releases[index].Identifiers, hint.Identifiers)
			continue
		}
		releaseIndexes[key] = len(releases)
		releases = append(releases, hint)
	}
	sort.Slice(releases, func(i, j int) bool {
		if releases[i].Title != releases[j].Title {
			return releases[i].Title < releases[j].Title
		}
		return releases[i].Year < releases[j].Year
	})
	request.Hints.Releases = releases
	return request
}

func normalizeIdentifier(identifier Identifier) Identifier {
	scheme := strings.ToLower(strings.TrimSpace(identifier.Scheme))
	aliases := map[string]string{
		"anidb_id": "anidb", "anilist_id": "anilist", "imdb_id": "imdb",
		"apple_artist": "apple", "apple_music_artist": "apple", "itunes_artist": "apple",
		"deezer_artist": "deezer", "discogs_artist": "discogs",
		"mal": "myanimelist", "mal_id": "myanimelist", "mbid": "musicbrainz",
		"musicbrainz_id": "musicbrainz", "thetvdb": "tvdb", "tmdb_id": "tmdb",
		"tvdb_id": "tvdb", "tvmaze_id": "tvmaze",
	}
	if canonical, ok := aliases[scheme]; ok {
		scheme = canonical
	}
	identifier.Scheme = scheme
	identifier.Value = strings.TrimSpace(identifier.Value)
	switch identifier.Scheme {
	case "imdb", "musicbrainz":
		identifier.Value = strings.ToLower(identifier.Value)
	case "isbn":
		identifier.Value = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(identifier.Value))
	case "anidb", "anilist", "apple", "deezer", "discogs", "googlebooks", "kitsu", "myanimelist", "tmdb", "tvdb", "tvmaze", "tvrage":
		if value, err := strconv.ParseInt(identifier.Value, 10, 64); err == nil && value > 0 {
			identifier.Value = strconv.FormatInt(value, 10)
		}
	}
	return identifier
}

func normalizeReleaseIdentifiers(values []Identifier) []Identifier {
	result := make([]Identifier, 0, len(values))
	seen := map[string]bool{}
	for _, identifier := range values {
		scheme := strings.ToLower(strings.TrimSpace(identifier.Scheme))
		switch scheme {
		case "itunes_album", "apple_album", "apple_music_album":
			scheme = "apple"
		case "deezer_album":
			scheme = "deezer"
		case "discogs_release", "discogs_master":
			// Keep the release/master distinction; a Discogs number is not
			// self-describing outside its namespace.
		default:
			identifier.Scheme = scheme
			identifier = normalizeIdentifier(identifier)
			scheme = identifier.Scheme
		}
		identifier.Scheme = scheme
		identifier.Value = strings.TrimSpace(identifier.Value)
		if scheme == "apple" || scheme == "deezer" || scheme == "discogs_release" || scheme == "discogs_master" {
			if value, err := strconv.ParseInt(identifier.Value, 10, 64); err == nil && value > 0 {
				identifier.Value = strconv.FormatInt(value, 10)
			}
		}
		key := identifier.Scheme + "\x00" + identifier.Value
		if identifier.Scheme == "" || identifier.Value == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, identifier)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Scheme != result[j].Scheme {
			return result[i].Scheme < result[j].Scheme
		}
		return result[i].Value < result[j].Value
	})
	return result
}

func mergeIdentifiers(left, right []Identifier) []Identifier {
	return normalizeReleaseIdentifiers(append(append([]Identifier(nil), left...), right...))
}
func scoreCandidate(request Request, candidate *Candidate) {
	score := float64(candidate.ProviderScore) / 100 * .22
	provider := candidate.Identity.Provider
	if provider == "" {
		provider = "upstream"
	}
	candidate.Evidence = append(candidate.Evidence, Evidence{Field: "provider_score", Outcome: "support", Weight: round(float64(candidate.ProviderScore) / 100 * .22), Detail: fmt.Sprintf("%s score %d/100", provider, candidate.ProviderScore)})
	query := normalizedText(request.Query)
	name := normalizedText(candidate.Display.Name)
	nameSimilarity := similarity(query, name)
	nameWeight := nameSimilarity * .38
	outcome := "fuzzy"
	if query == name {
		outcome = "exact"
		nameWeight = .38
	} else {
		for _, alias := range candidate.Display.Aliases {
			if query == normalizedText(alias) {
				outcome = "exact_alias"
				// A locale or transliteration alias is exact identity evidence,
				// not a weaker spelling of the primary display name.
				nameWeight = .38
				break
			}
		}
	}
	score += nameWeight
	candidate.Evidence = append(candidate.Evidence, Evidence{Field: "name", Outcome: outcome, Weight: round(nameWeight), Detail: candidate.Display.Name})
	if country := request.Hints.Country; country != "" {
		weight := -.06
		outcome := "mismatch"
		if country == candidate.Display.Country {
			weight = .11
			outcome = "exact"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "country", Outcome: outcome, Weight: weight, Detail: candidate.Display.Country})
	}
	if kind := request.Hints.Type; kind != "" {
		weight := -.04
		outcome := "mismatch"
		if kind == candidate.Display.Type {
			weight = .08
			outcome = "exact"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "type", Outcome: outcome, Weight: weight, Detail: candidate.Display.Type})
	}
	if begin := request.Hints.BeginDate; begin != "" {
		weight := -.04
		outcome := "mismatch"
		if begin == candidate.Display.BeginDate {
			weight = .1
			outcome = "exact"
		} else if len(begin) >= 4 && len(candidate.Display.BeginDate) >= 4 && begin[:4] == candidate.Display.BeginDate[:4] {
			weight = .06
			outcome = "year_match"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "begin_date", Outcome: outcome, Weight: weight, Detail: candidate.Display.BeginDate})
	}
	if area := request.Hints.Area; area != "" {
		weight, outcome := -.03, "mismatch"
		if normalizedText(area) == normalizedText(candidate.Display.Area) {
			weight, outcome = .07, "exact"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "area", Outcome: outcome, Weight: weight, Detail: candidate.Display.Area})
	}
	if end := request.Hints.EndDate; end != "" {
		weight, outcome := -.03, "mismatch"
		if end == candidate.Display.EndDate {
			weight, outcome = .07, "exact"
		} else if len(end) >= 4 && len(candidate.Display.EndDate) >= 4 && end[:4] == candidate.Display.EndDate[:4] {
			weight, outcome = .04, "year_match"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "end_date", Outcome: outcome, Weight: weight, Detail: candidate.Display.EndDate})
	}
	if len(request.Hints.Aliases) > 0 {
		matched := 0
		names := append([]string{candidate.Display.Name}, candidate.Display.Aliases...)
		for _, hint := range request.Hints.Aliases {
			for _, name := range names {
				if normalizedText(hint) == normalizedText(name) {
					matched++
					break
				}
			}
		}
		weight := .1 * float64(matched) / float64(len(request.Hints.Aliases))
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "aliases", Outcome: fmt.Sprintf("%d_of_%d", matched, len(request.Hints.Aliases)), Weight: round(weight)})
	}
	if count := len(request.Hints.Releases); count > 0 {
		weight := .25 * float64(len(candidate.MatchedReleases)) / float64(count)
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "releases", Outcome: fmt.Sprintf("%d_of_%d", len(candidate.MatchedReleases), count), Weight: round(weight)})
	}
	candidate.Confidence = round(math.Max(0, math.Min(.99, score)))
	switch {
	case candidate.Confidence >= .85:
		candidate.Match = "strong"
	case candidate.Confidence >= .65:
		candidate.Match = "likely"
	case candidate.Confidence >= .45:
		candidate.Match = "possible"
	default:
		candidate.Match = "weak"
	}
}

func dedupeExistingArtistCandidates(values []Candidate) []Candidate {
	result := make([]Candidate, 0, len(values))
	byEntity := map[string]int{}
	for _, value := range values {
		if value.ExistingEntityID == "" {
			result = append(result, value)
			continue
		}
		index, exists := byEntity[value.ExistingEntityID]
		if !exists {
			byEntity[value.ExistingEntityID] = len(result)
			result = append(result, value)
			continue
		}
		matches := appendUniqueArtistReleaseMatches(result[index].artistReleaseMatches, value.artistReleaseMatches...)
		hints := result[index].MatchedReleases
		for _, hint := range value.MatchedReleases {
			hints = appendUniqueReleaseHint(hints, hint)
		}
		if value.Confidence > result[index].Confidence {
			result[index] = value
		}
		result[index].artistReleaseMatches = matches
		result[index].MatchedReleases = hints
	}
	return result
}

// Provider full-text searches can return high provider scores for a single
// token in a longer credited artist (for example a dozen distinct artists
// named "Above" for "Above & Beyond ft Zoe Johnston"). Those are useful
// upstream recall but not plausible identity candidates and used to flood the
// consumer's review queue. Keep exact names/aliases and structured release
// matches even when a provider supplies a poor score; otherwise expose only
// candidates that reached at least the service's "possible" evidence tier.
func filterWeakArtistCandidates(request Request, values []Candidate) []Candidate {
	query := normalizedText(request.Query)
	result := make([]Candidate, 0, len(values))
	for _, candidate := range values {
		exact := query != "" && query == normalizedText(candidate.Display.Name)
		for _, alias := range candidate.Display.Aliases {
			exact = exact || (query != "" && query == normalizedText(alias))
		}
		if candidate.Match != "weak" || exact || len(candidate.MatchedReleases) > 0 {
			result = append(result, candidate)
		}
	}
	return result
}

type artistReleaseBridge struct {
	EntityID    string
	MusicBrainz artistReleaseMatch
	Storefront  artistReleaseMatch
}

type artistReleaseBridgeChecker func(context.Context, artistReleaseBridge) (bool, error)

// consolidatePersistedArtistCandidates can anchor one new MusicBrainz root to
// one existing storefront artist, but only when persisted canonical evidence
// joins both sides through the same release-group UUID. Search names and
// title/year matches only select which hard evidence to inspect; they never
// establish identity, and no two unclaimed roots are collapsed here.
func (s *Service) consolidatePersistedArtistCandidates(ctx context.Context, request Request, values []Candidate) ([]Candidate, *ArtistIdentityConvergence, error) {
	return consolidateArtistCandidatesWithBridge(ctx, request, values, s.persistedArtistReleaseBridge)
}

func consolidateArtistCandidatesWithBridge(ctx context.Context, request Request, values []Candidate, check artistReleaseBridgeChecker) ([]Candidate, *ArtistIdentityConvergence, error) {
	if len(request.Hints.Releases) == 0 || normalizedText(request.Query) == "" || check == nil {
		return values, nil, nil
	}

	anchorByEntity := map[string][]int{}
	var musicBrainz []int
	for index, candidate := range values {
		if !artistCandidateExact(request.Query, candidate) || !artistReleaseMatchesCoverHints(candidate.artistReleaseMatches, request.Hints.Releases) {
			continue
		}
		provider := strings.ToLower(strings.TrimSpace(candidate.Identity.Provider))
		switch {
		case provider == "musicbrainz" && candidate.ExistingEntityID == "":
			musicBrainz = append(musicBrainz, index)
		case (provider == "apple" || provider == "deezer") && candidate.ExistingEntityID != "":
			anchorByEntity[candidate.ExistingEntityID] = append(anchorByEntity[candidate.ExistingEntityID], index)
		}
	}
	if len(anchorByEntity) != 1 || len(musicBrainz) != 1 {
		return values, nil, nil
	}
	var entityID string
	var anchorIndices []int
	for id, indices := range anchorByEntity {
		entityID, anchorIndices = id, indices
	}
	anchorIndex := anchorIndices[0]
	for _, index := range anchorIndices[1:] {
		if values[index].Confidence > values[anchorIndex].Confidence {
			anchorIndex = index
		}
	}
	mbIndex := musicBrainz[0]
	mbCandidate := values[mbIndex]
	anchor := values[anchorIndex]
	var materializationBridge artistReleaseBridge

	for _, hint := range request.Hints.Releases {
		hintKey := releaseHintIdentityKey(hint)
		proved := false
		for _, mbMatch := range mbCandidate.artistReleaseMatches {
			if mbMatch.HintKey != hintKey || mbMatch.Release.Provider != "musicbrainz" || mbMatch.Release.Namespace != "release_group" {
				continue
			}
			for _, storefrontMatch := range anchor.artistReleaseMatches {
				provider := strings.ToLower(strings.TrimSpace(storefrontMatch.Release.Provider))
				if storefrontMatch.HintKey != hintKey || (provider != "apple" && provider != "deezer") || storefrontMatch.Artist.Provider != provider {
					continue
				}
				matched, err := check(ctx, artistReleaseBridge{EntityID: entityID, MusicBrainz: mbMatch, Storefront: storefrontMatch})
				if err != nil {
					return nil, nil, err
				}
				if matched {
					proved = true
					if materializationBridge.EntityID == "" {
						materializationBridge = artistReleaseBridge{EntityID: entityID, MusicBrainz: mbMatch, Storefront: storefrontMatch}
					}
					break
				}
			}
			if proved {
				break
			}
		}
		if !proved {
			return values, nil, nil
		}
	}

	chosen := anchor
	chosen.Confidence = .99
	chosen.Match = "strong"
	chosen.Evidence = append(chosen.Evidence, Evidence{
		Field:   "canonical_release_claims",
		Outcome: "persisted_cross_provider_bridge",
		Detail:  "accepted storefront artist and release claims converge with the MusicBrainz credit on the same canonical release group",
	})
	result := make([]Candidate, 0, len(values)-1)
	for index, candidate := range values {
		if index == mbIndex {
			continue
		}
		if index == anchorIndex {
			candidate = chosen
		}
		result = append(result, candidate)
	}
	return result, &ArtistIdentityConvergence{
		EntityID:                   entityID,
		MusicBrainzID:              mbCandidate.Identity.Value,
		MusicBrainzReleaseGroupID:  materializationBridge.MusicBrainz.Release.Value,
		StorefrontProvider:         materializationBridge.Storefront.Release.Provider,
		StorefrontArtistID:         materializationBridge.Storefront.Artist.Value,
		StorefrontReleaseNamespace: materializationBridge.Storefront.Release.Namespace,
		StorefrontReleaseID:        materializationBridge.Storefront.Release.Value,
	}, nil
}

func (s *Service) persistedArtistReleaseBridge(ctx context.Context, bridge artistReleaseBridge) (bool, error) {
	storefrontProvider := strings.ToLower(strings.TrimSpace(bridge.Storefront.Release.Provider))
	if bridge.EntityID == "" || storefrontProvider == "" || storefrontProvider != strings.ToLower(strings.TrimSpace(bridge.Storefront.Artist.Provider)) {
		return false, nil
	}
	return musiccatalog.PersistedArtistIdentityBridge(ctx, s.runtime, musiccatalog.ArtistIdentityBridge{
		ArtistEntityID:             bridge.EntityID,
		MusicBrainzArtistID:        bridge.MusicBrainz.Artist.Value,
		MusicBrainzReleaseGroupID:  bridge.MusicBrainz.Release.Value,
		StorefrontProvider:         storefrontProvider,
		StorefrontArtistID:         bridge.Storefront.Artist.Value,
		StorefrontReleaseNamespace: bridge.Storefront.Release.Namespace,
		StorefrontReleaseID:        bridge.Storefront.Release.Value,
	})
}

func artistCandidateExact(query string, candidate Candidate) bool {
	want := normalizedText(query)
	if want == "" {
		return false
	}
	if normalizedText(candidate.Display.Name) == want {
		return true
	}
	for _, alias := range candidate.Display.Aliases {
		if normalizedText(alias) == want {
			return true
		}
	}
	return false
}

func artistReleaseMatchesCoverHints(matches []artistReleaseMatch, hints []ReleaseHint) bool {
	covered := map[string]bool{}
	for _, match := range matches {
		if strings.TrimSpace(match.Release.Value) != "" {
			covered[match.HintKey] = true
		}
	}
	for _, hint := range hints {
		if !covered[releaseHintIdentityKey(hint)] {
			return false
		}
	}
	return len(hints) > 0
}

func releaseHintIdentityKey(hint ReleaseHint) string {
	return normalizedText(hint.Title) + "\x00" + strconv.Itoa(hint.Year) + "\x00" + normalizeType(hint.Type)
}

func appendUniqueArtistReleaseMatch(values []artistReleaseMatch, value artistReleaseMatch) []artistReleaseMatch {
	return appendUniqueArtistReleaseMatches(values, value)
}

func appendUniqueArtistReleaseMatches(values []artistReleaseMatch, additions ...artistReleaseMatch) []artistReleaseMatch {
	for _, addition := range additions {
		key := addition.HintKey + "\x00" + addition.Artist.Provider + "\x00" + addition.Artist.Namespace + "\x00" + addition.Artist.Value + "\x00" + addition.Release.Provider + "\x00" + addition.Release.Namespace + "\x00" + addition.Release.Value
		duplicate := false
		for _, existing := range values {
			existingKey := existing.HintKey + "\x00" + existing.Artist.Provider + "\x00" + existing.Artist.Namespace + "\x00" + existing.Artist.Value + "\x00" + existing.Release.Provider + "\x00" + existing.Release.Namespace + "\x00" + existing.Release.Value
			if existingKey == key {
				duplicate = true
				break
			}
		}
		if !duplicate {
			values = append(values, addition)
		}
	}
	return values
}

func recommendation(values []Candidate) string {
	if len(values) == 0 {
		return "no_match"
	}
	margin := values[0].Confidence
	if len(values) > 1 {
		margin -= values[1].Confidence
	}
	if values[0].Confidence >= .85 && margin >= .12 {
		return "strong_match"
	}
	if values[0].Confidence >= .65 && margin >= .08 {
		return "likely_match"
	}
	return "ambiguous"
}

// presentCandidates keeps the identity decision independent from the caller's
// presentation limit. In particular, limit=1 must not hide a tied runner-up
// and turn an ambiguous result into a strong match.
func presentCandidates(values []Candidate, limit int) (string, []Candidate) {
	recommended := recommendation(values)
	if len(values) > limit {
		values = values[:limit]
	}
	for index := range values {
		values[index].Rank = index + 1
	}
	return recommended, values
}
func normalizedText(value string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}
func similarity(a, b string) float64 {
	left, right := []rune(a), []rune(b)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	previous := make([]int, len(right)+1)
	for j := range previous {
		previous[j] = j
	}
	for i, lr := range left {
		current := make([]int, len(right)+1)
		current[0] = i + 1
		for j, rr := range right {
			cost := 1
			if lr == rr {
				cost = 0
			}
			current[j+1] = min(current[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous = current
	}
	distance := previous[len(right)]
	return math.Max(0, 1-float64(distance)/float64(max(len(left), len(right))))
}
func cleanSorted(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := normalizedText(value)
		if key != "" && !seen[key] {
			seen[key] = true
			out = append(out, value)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
func cleanSortedLower(values []string) []string {
	for i := range values {
		values[i] = strings.ToLower(strings.TrimSpace(values[i]))
	}
	return cleanSorted(values)
}
func appendUniqueReleaseHint(values []ReleaseHint, hint ReleaseHint) []ReleaseHint {
	for index, existing := range values {
		if normalizedText(existing.Title) == normalizedText(hint.Title) && existing.Year == hint.Year && existing.Type == hint.Type {
			values[index].Identifiers = mergeIdentifiers(existing.Identifiers, hint.Identifiers)
			return values
		}
	}
	return append(values, hint)
}
func normalizeType(value string) string {
	return strings.NewReplacer(" ", "_", "-", "_").Replace(strings.ToLower(strings.TrimSpace(value)))
}
func escapeLucene(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}
func releaseYear(value string) int {
	if len(value) < 4 {
		return 0
	}
	year, _ := strconv.Atoi(value[:4])
	return year
}
func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
func round(value float64) float64 { return math.Round(value*1000) / 1000 }
