// Package musiccatalog builds a provider-neutral artist discography. Provider
// catalog rows are evidence; the clustered result is the public relationship.
package musiccatalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/apple"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/deezer"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/discogs"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/lastfm"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/musicbrainz"
	"github.com/HeyaMedia/HeyaMetadata/internal/releasegroups"
	"github.com/HeyaMedia/HeyaMetadata/internal/textmatch"
	"github.com/jackc/pgx/v5"
)

const SyncVersion = "mixed-artist-catalog/v10"

type ReleaseGroup struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	FirstRelease   string   `json:"first-release-date"`
	PrimaryType    string   `json:"primary-type"`
	SecondaryTypes []string `json:"secondary-types"`
}
type candidate struct {
	Provider, Namespace, ID string
	Title, Date, Kind       string
	ArtistName, URL         string
	ObservationID           string
	ObservedAt              time.Time
	MatchReason             string
	MatchConfidence         float64
	Metadata                map[string]any
}
type cluster struct {
	Sources          []candidate
	BridgeReason     string
	BridgeConfidence float64
	PromotionState   string
	PromotionError   string
}
type Result struct {
	ReleaseGroups []ReleaseGroup
	Pages         int
	Candidates    int
	Clusters      int
}

func SyncArtist(ctx context.Context, runtime *platform.Runtime, artistEntityID, mbid string, jobID int64) (result Result, returnErr error) {
	mbid = strings.ToLower(strings.TrimSpace(mbid))
	if _, err := runtime.DB.Exec(ctx, `INSERT INTO artist_catalog_sync_runs(river_job_id,artist_entity_id,musicbrainz_id,sync_version,state)VALUES($1,$2,$3,$4,'working')ON CONFLICT(river_job_id)DO UPDATE SET sync_version=EXCLUDED.sync_version,state='working',pages=0,release_groups=0,error=NULL,completed_at=NULL`, jobID, artistEntityID, mbid, SyncVersion); err != nil {
		return result, err
	}
	defer func() {
		if returnErr != nil {
			_, _ = runtime.DB.Exec(context.WithoutCancel(ctx), `UPDATE artist_catalog_sync_runs SET state='failed',error=$2,pages=$3,release_groups=$4,completed_at=now()WHERE river_job_id=$1`, jobID, returnErr.Error(), result.Pages, result.Clusters)
		}
	}()

	aliases, claims, err := artistContext(ctx, runtime, artistEntityID)
	if err != nil {
		return result, err
	}
	mb, pages, err := collectMusicBrainz(ctx, runtime, mbid, jobID)
	if err != nil {
		return result, err
	}
	result.Pages = pages
	sets := map[string]map[string][]candidate{"musicbrainz": {mbid: mb}}
	if ids := claims["apple"]; len(ids) > 0 {
		sets["apple"], err = collectApple(ctx, runtime, ids, jobID)
		if err != nil {
			return result, err
		}
	}
	if ids := claims["deezer"]; len(ids) > 0 {
		sets["deezer"], err = collectDeezer(ctx, runtime, ids, jobID)
		if err != nil {
			return result, err
		}
	}
	if ids := claims["discogs"]; len(ids) > 0 {
		sets["discogs"], err = collectDiscogs(ctx, runtime, ids, jobID)
		if err != nil {
			return result, err
		}
	}
	if runtime.Config.Providers.LastFM.APIKey != "" {
		sets["lastfm"], err = collectLastFM(ctx, runtime, mbid, jobID)
		if err != nil {
			return result, err
		}
	}

	selected := selectProviderIdentities(sets, aliases)
	all := append([]candidate(nil), mb...)
	for _, provider := range []string{"discogs", "apple", "deezer", "lastfm"} {
		all = append(all, selected[provider]...)
	}
	result.Candidates = len(all)
	clusters := clusterCandidates(all)
	clusters = enrichClustersWithDetailEvidence(ctx, runtime, clusters, jobID)
	result.Clusters = len(clusters)
	_, _ = runtime.DB.Exec(ctx, `UPDATE artist_catalog_promotions SET state='superseded',updated_at=now() WHERE artist_entity_id=$1`, artistEntityID)
	promoteProviderOnlyClusters(ctx, runtime, artistEntityID, clusters)
	if err := persistClusters(ctx, runtime, artistEntityID, clusters); err != nil {
		return result, err
	}
	if err := reconcilePromotions(ctx, runtime, artistEntityID); err != nil {
		return result, err
	}
	for _, c := range mb {
		result.ReleaseGroups = append(result.ReleaseGroups, ReleaseGroup{ID: c.ID, Title: c.Title, FirstRelease: c.Date, PrimaryType: c.Kind})
	}
	_, err = runtime.DB.Exec(ctx, `UPDATE artist_catalog_sync_runs SET state='completed',pages=$2,release_groups=$3,error=NULL,completed_at=now()WHERE river_job_id=$1`, jobID, result.Pages, result.Clusters)
	return result, err
}

func artistContext(ctx context.Context, runtime *platform.Runtime, entityID string) ([]string, map[string][]string, error) {
	aliases := []string{}
	rows, err := runtime.DB.Query(ctx, `SELECT value FROM search_names WHERE entity_id=$1 ORDER BY source_quality DESC`, entityID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var v string
		if err = rows.Scan(&v); err != nil {
			rows.Close()
			return nil, nil, err
		}
		aliases = append(aliases, v)
	}
	rows.Close()
	claims := map[string][]string{}
	rows, err = runtime.DB.Query(ctx, `SELECT provider,normalized_value FROM external_id_claims WHERE entity_id=$1 AND entity_kind='artist' AND namespace='artist' AND provider IN('apple','deezer','discogs') AND state='accepted' ORDER BY provider,normalized_value`, entityID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p, v string
		if err = rows.Scan(&p, &v); err != nil {
			return nil, nil, err
		}
		claims[p] = append(claims[p], v)
	}
	return aliases, claims, rows.Err()
}

func resolver(runtime *platform.Runtime, cap providers.Capability, jobID int64) (providers.PayloadResolver, error) {
	return providercache.New(runtime, SyncVersion, cap.RawRetention, cap.ResponseCache, jobID)
}

func collectMusicBrainz(ctx context.Context, runtime *platform.Runtime, mbid string, jobID int64) ([]candidate, int, error) {
	base := musicbrainz.New(runtime.Config.Providers.MusicBrainz)
	r, err := resolver(runtime, base.Capability(), jobID)
	if err != nil {
		return nil, 0, err
	}
	client := musicbrainz.NewCached(runtime.Config.Providers.MusicBrainz, r)
	out := []candidate{}
	pages := 0
	for offset := 0; ; {
		p, err := client.BrowseReleaseGroups(ctx, mbid, 100, offset)
		if err != nil {
			return nil, pages, err
		}
		if p.StatusCode != http.StatusOK {
			return nil, pages, &providers.StatusError{Provider: "musicbrainz", StatusCode: p.StatusCode}
		}
		var body struct {
			Count  int            `json:"release-group-count"`
			Groups []ReleaseGroup `json:"release-groups"`
		}
		if err = json.Unmarshal(p.Body, &body); err != nil {
			return nil, pages, fmt.Errorf("decode MusicBrainz catalog: %w", err)
		}
		pages++
		for _, g := range body.Groups {
			if g.ID != "" && strings.TrimSpace(g.Title) != "" {
				out = append(out, candidate{Provider: "musicbrainz", Namespace: "release_group", ID: strings.ToLower(g.ID), Title: g.Title, Date: g.FirstRelease, Kind: normalizeKind(g.PrimaryType), ObservationID: p.ObservationID, ObservedAt: p.ObservedAt, MatchReason: "provider_record", MatchConfidence: 1, Metadata: map[string]any{"secondary_types": g.SecondaryTypes}})
			}
		}
		offset += len(body.Groups)
		if len(body.Groups) == 0 || offset >= body.Count {
			break
		}
		if pages >= 100 {
			return nil, pages, fmt.Errorf("MusicBrainz catalog exceeded 100 pages")
		}
	}
	return out, pages, nil
}

func collectApple(ctx context.Context, runtime *platform.Runtime, ids []string, jobID int64) (map[string][]candidate, error) {
	base := apple.New(runtime.Config.Providers.Apple)
	r, err := resolver(runtime, base.Capability(), jobID)
	if err != nil {
		return nil, err
	}
	client := apple.NewCached(runtime.Config.Providers.Apple, r, "")
	out := map[string][]candidate{}
	for _, id := range ids {
		ps, err := client.Collect(ctx, providers.Identifier{Provider: "apple", Namespace: "artist", Value: id})
		if err != nil {
			return nil, err
		}
		if len(ps) == 0 || ps[0].StatusCode != 200 {
			continue
		}
		p := ps[0]
		var body struct {
			Results []struct {
				WrapperType    string `json:"wrapperType"`
				CollectionID   int64  `json:"collectionId"`
				CollectionName string `json:"collectionName"`
				ArtistID       int64  `json:"artistId"`
				ArtistName     string `json:"artistName"`
				ReleaseDate    string `json:"releaseDate"`
				CollectionType string `json:"collectionType"`
				TrackCount     int    `json:"trackCount"`
				URL            string `json:"collectionViewUrl"`
			} `json:"results"`
		}
		if err = json.Unmarshal(p.Body, &body); err != nil {
			return nil, err
		}
		for _, v := range body.Results {
			if v.WrapperType != "collection" || strconv.FormatInt(v.ArtistID, 10) != id || v.CollectionID < 1 || v.CollectionName == "" {
				continue
			}
			title, kind := storefrontTitle(v.CollectionName, v.CollectionType, v.TrackCount)
			out[id] = append(out[id], candidate{Provider: "apple", Namespace: "album", ID: strconv.FormatInt(v.CollectionID, 10), Title: title, Date: v.ReleaseDate, Kind: kind, ArtistName: v.ArtistName, URL: v.URL, ObservationID: p.ObservationID, ObservedAt: p.ObservedAt, Metadata: map[string]any{"track_count": v.TrackCount, "storefront_title": v.CollectionName}})
		}
	}
	return out, nil
}

func collectDeezer(ctx context.Context, runtime *platform.Runtime, ids []string, jobID int64) (map[string][]candidate, error) {
	base := deezer.New(runtime.Config.Providers.Deezer)
	r, err := resolver(runtime, base.Capability(), jobID)
	if err != nil {
		return nil, err
	}
	client := deezer.NewCached(runtime.Config.Providers.Deezer, r)
	out := map[string][]candidate{}
	for _, id := range ids {
		for index := 0; ; {
			p, err := client.ArtistAlbums(ctx, id, 200, index)
			if err != nil {
				return nil, err
			}
			if p.StatusCode == http.StatusNotFound {
				break
			}
			if p.StatusCode != 200 {
				return nil, &providers.StatusError{Provider: "deezer", StatusCode: p.StatusCode}
			}
			var body struct {
				Total int `json:"total"`
				Data  []struct {
					ID          int64  `json:"id"`
					Title       string `json:"title"`
					Link        string `json:"link"`
					ReleaseDate string `json:"release_date"`
					RecordType  string `json:"record_type"`
					TrackCount  int    `json:"nb_tracks"`
					Artist      struct {
						ID   int64  `json:"id"`
						Name string `json:"name"`
					} `json:"artist"`
				} `json:"data"`
			}
			if err = json.Unmarshal(p.Body, &body); err != nil {
				return nil, err
			}
			for _, v := range body.Data {
				if v.ID < 1 || v.Title == "" {
					continue
				}
				out[id] = append(out[id], candidate{Provider: "deezer", Namespace: "album", ID: strconv.FormatInt(v.ID, 10), Title: v.Title, Date: v.ReleaseDate, Kind: normalizeKind(v.RecordType), ArtistName: v.Artist.Name, URL: v.Link, ObservationID: p.ObservationID, ObservedAt: p.ObservedAt, Metadata: map[string]any{"track_count": v.TrackCount}})
			}
			index += len(body.Data)
			if len(body.Data) == 0 || index >= body.Total {
				break
			}
		}
	}
	return out, nil
}

func collectDiscogs(ctx context.Context, runtime *platform.Runtime, ids []string, jobID int64) (map[string][]candidate, error) {
	base := discogs.New(runtime.Config.Providers.Discogs)
	r, err := resolver(runtime, base.Capability(), jobID)
	if err != nil {
		return nil, err
	}
	client := discogs.NewCached(runtime.Config.Providers.Discogs, r, "")
	out := map[string][]candidate{}
	for _, id := range ids {
		for page := 1; ; page++ {
			p, err := client.ArtistReleases(ctx, id, 100, page)
			if err != nil {
				return nil, err
			}
			if p.StatusCode == http.StatusNotFound {
				break
			}
			if p.StatusCode != 200 {
				return nil, &providers.StatusError{Provider: "discogs", StatusCode: p.StatusCode}
			}
			var body struct {
				Pagination struct {
					Pages int `json:"pages"`
				} `json:"pagination"`
				Releases []struct {
					ID          int64  `json:"id"`
					Type        string `json:"type"`
					Title       string `json:"title"`
					Artist      string `json:"artist"`
					Role        string `json:"role"`
					Year        int    `json:"year"`
					MainRelease int64  `json:"main_release"`
					ResourceURL string `json:"resource_url"`
					Format      string `json:"format"`
				} `json:"releases"`
			}
			if err = json.Unmarshal(p.Body, &body); err != nil {
				return nil, err
			}
			for _, v := range body.Releases {
				if v.ID < 1 || v.Title == "" || v.Role != "" && !strings.EqualFold(v.Role, "Main") {
					continue
				}
				ns := "release"
				if v.Type == "master" {
					ns = "master"
				}
				out[id] = append(out[id], candidate{Provider: "discogs", Namespace: ns, ID: strconv.FormatInt(v.ID, 10), Title: v.Title, Date: yearString(v.Year), Kind: normalizeKind(v.Format), ArtistName: v.Artist, URL: v.ResourceURL, ObservationID: p.ObservationID, ObservedAt: p.ObservedAt, Metadata: map[string]any{"main_release": v.MainRelease, "format": v.Format}})
			}
			if page >= body.Pagination.Pages || page >= 100 {
				break
			}
		}
	}
	return out, nil
}

func collectLastFM(ctx context.Context, runtime *platform.Runtime, mbid string, jobID int64) (map[string][]candidate, error) {
	base := lastfm.New(runtime.Config.Providers.LastFM)
	r, err := resolver(runtime, base.Capability(), jobID)
	if err != nil {
		return nil, err
	}
	client := lastfm.NewCached(runtime.Config.Providers.LastFM, r, "")
	out := []candidate{}
	for page := 1; ; page++ {
		p, err := client.ArtistTopAlbums(ctx, mbid, 200, page)
		if err != nil {
			return nil, err
		}
		if p.StatusCode != 200 {
			return nil, &providers.StatusError{Provider: "lastfm", StatusCode: p.StatusCode}
		}
		var body struct {
			Top struct {
				Albums []struct {
					Name, MBID, URL string
					Playcount       any
					Artist          struct{ Name, MBID string }
				} `json:"album"`
				Attr struct {
					TotalPages string `json:"totalPages"`
				} `json:"@attr"`
			} `json:"topalbums"`
		}
		if err = json.Unmarshal(p.Body, &body); err != nil {
			return nil, err
		}
		for _, v := range body.Top.Albums {
			if v.Name == "" {
				continue
			}
			id := strings.ToLower(v.MBID)
			ns := "release_group"
			if id == "" {
				sum := sha256.Sum256([]byte(strings.ToLower(v.Artist.Name + "\x00" + v.Name)))
				id = hex.EncodeToString(sum[:12])
				ns = "album_name"
			}
			out = append(out, candidate{Provider: "lastfm", Namespace: ns, ID: id, Title: v.Name, ArtistName: v.Artist.Name, URL: v.URL, ObservationID: p.ObservationID, ObservedAt: p.ObservedAt, Metadata: map[string]any{"playcount": v.Playcount}})
		}
		total, _ := strconv.Atoi(body.Top.Attr.TotalPages)
		if page >= total || page >= 20 {
			break
		}
	}
	return map[string][]candidate{mbid: out}, nil
}

func selectProviderIdentities(sets map[string]map[string][]candidate, aliases []string) map[string][]candidate {
	selected := map[string][]candidate{"musicbrainz": firstSet(sets["musicbrainz"])}
	anchors := append([]candidate(nil), selected["musicbrainz"]...)
	// Trust is directional. Two same-name storefront catalogs are not allowed
	// to validate each other without first overlapping an anchored catalog.
	for _, p := range []string{"discogs", "apple", "deezer", "lastfm"} {
		choices := sets[p]
		if len(choices) == 0 {
			continue
		}
		best, bestScore, tied := "", 0, false
		for id, v := range choices {
			if !catalogArtistCompatible(v, aliases) {
				continue
			}
			score := overlap(v, anchors)
			if score > bestScore {
				best, bestScore, tied = id, score, false
			} else if score == bestScore && score > 0 {
				tied = true
			}
		}
		if bestScore > 0 && !tied {
			selected[p] = choices[best]
			anchors = append(anchors, choices[best]...)
		}
	}
	return selected
}
func firstSet(values map[string][]candidate) []candidate {
	for _, v := range values {
		return v
	}
	return nil
}
func catalogArtistCompatible(values []candidate, aliases []string) bool {
	sawName := false
	for _, v := range values {
		if v.ArtistName == "" {
			continue
		}
		sawName = true
		for _, a := range aliases {
			if textmatch.EquivalentRelease(stripDiscogsSuffix(v.ArtistName), 0, a, 0) {
				return true
			}
		}
	}
	return len(values) > 0 && (!sawName || len(aliases) == 0)
}
func overlap(a, b []candidate) int {
	seen := map[int]bool{}
	n := 0
	for _, x := range a {
		for i, y := range b {
			if !seen[i] && (compatible(x, y) || exactDateCrossScript(x, y)) {
				seen[i] = true
				n++
				break
			}
		}
	}
	return n
}
func clusterCandidates(values []candidate) []cluster {
	sort.SliceStable(values, func(i, j int) bool { return providerRank(values[i].Provider) < providerRank(values[j].Provider) })
	out := []cluster{}
	for _, v := range values {
		matched := -1
		matchReason := ""
		matchConfidence := 0.0
		for i, c := range out {
			if ok, reason, confidence := candidateMatch(v, c.Sources[0]); ok {
				matched = i
				matchReason, matchConfidence = reason, confidence
				break
			}
		}
		if matched < 0 {
			dateMatches := []int{}
			for i, c := range out {
				if exactDateCrossScript(v, c.Sources[0]) {
					dateMatches = append(dateMatches, i)
				}
			}
			// An exact date and release type can bridge an untranslated title,
			// but only when it identifies one existing cluster unambiguously.
			if len(dateMatches) == 1 {
				matched = dateMatches[0]
				matchReason, matchConfidence = "unique_exact_date_type_cross_script", 0.88
			}
		}
		if matched < 0 && v.Provider == "lastfm" {
			continue
		}
		if matched < 0 {
			v.MatchReason, v.MatchConfidence = "provider_record", providerRecordConfidence(v.Provider)
			out = append(out, cluster{Sources: []candidate{v}})
		} else if !hasSource(out[matched], v) {
			v.MatchReason, v.MatchConfidence = matchReason, matchConfidence
			out[matched].Sources = append(out[matched].Sources, v)
		}
	}
	return out
}

func hasSource(group cluster, value candidate) bool {
	for _, existing := range group.Sources {
		if existing.Provider == value.Provider && existing.Namespace == value.Namespace && existing.ID == value.ID {
			return true
		}
	}
	return false
}

func exactDateCrossScript(a, b candidate) bool {
	if a.Provider == b.Provider || a.Kind == "" || a.Kind != b.Kind || len(a.Date) < 10 || len(b.Date) < 10 || a.Date[:10] != b.Date[:10] {
		return false
	}
	return ascii(a.Title) != ascii(b.Title)
}

func ascii(value string) bool {
	for _, r := range value {
		if r > 127 {
			return false
		}
	}
	return true
}
func compatible(a, b candidate) bool {
	ok, _, _ := candidateMatch(a, b)
	return ok
}

func candidateMatch(a, b candidate) (bool, string, float64) {
	ay, by := year(a.Date), year(b.Date)
	if a.Kind != "" && b.Kind != "" && a.Kind != b.Kind {
		return false, "", 0
	}
	if a.Provider == "musicbrainz" && b.Provider == "musicbrainz" {
		if textmatch.EquivalentRelease(a.Title, ay, b.Title, by) {
			return true, "musicbrainz_title_year", 0.99
		}
		return false, "", 0
	}
	if textmatch.EquivalentRelease(a.Title, 0, b.Title, 0) {
		if ay > 0 && by > 0 && ay == by {
			return true, "normalized_title_type_year", 0.96
		}
		return true, "normalized_title_type", 0.91
	}
	return false, "", 0
}

func stripDiscogsSuffix(value string) string {
	value = strings.TrimSpace(value)
	open := strings.LastIndex(value, " (")
	if open < 0 || !strings.HasSuffix(value, ")") {
		return value
	}
	if _, err := strconv.Atoi(value[open+2 : len(value)-1]); err == nil {
		return value[:open]
	}
	return value
}
func normalizeKind(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch {
	case strings.Contains(v, "single"):
		return "single"
	case strings.Contains(v, "maxi") || strings.Contains(v, `12"`) || strings.Contains(v, `7"`):
		return "single"
	case strings.Contains(v, "ep"):
		return "ep"
	case strings.Contains(v, "album") || strings.Contains(v, "lp") || strings.Contains(v, "live") || strings.Contains(v, "liv"):
		return "album"
	}
	return ""
}

func storefrontTitle(title, kind string, trackCount int) (string, string) {
	title = strings.TrimSpace(title)
	lower := strings.ToLower(title)
	for _, suffix := range []struct{ text, kind string }{{" - single", "single"}, {" - ep", "ep"}} {
		if strings.HasSuffix(lower, suffix.text) {
			return strings.TrimSpace(title[:len(title)-len(suffix.text)]), suffix.kind
		}
	}
	normalizedKind := normalizeKind(kind)
	if normalizedKind == "album" && trackCount > 0 && trackCount <= 3 {
		normalizedKind = "single"
	} else if normalizedKind == "album" && trackCount >= 4 && trackCount <= 6 {
		normalizedKind = "ep"
	}
	return title, normalizedKind
}
func providerRecordConfidence(provider string) float64 {
	if provider == "musicbrainz" {
		return 1
	}
	if provider == "lastfm" {
		return 0.5
	}
	return 0.72
}

func promoteProviderOnlyClusters(ctx context.Context, runtime *platform.Runtime, artistID string, clusters []cluster) {
	service := releasegroups.NewService(runtime)
	for i := range clusters {
		group := &clusters[i]
		if hasProvider(*group, "musicbrainz") {
			group.PromotionState = "musicbrainz_spine"
			continue
		}
		providers := authoritativeProviders(*group)
		if len(providers) < 2 || !providers["discogs"] {
			group.PromotionState = "unresolved_single_provider"
			continue
		}
		targets, err := clusterTargets(ctx, runtime, *group)
		if err != nil {
			group.PromotionState, group.PromotionError = "promotion_failed", err.Error()
			continue
		}
		if len(targets) > 1 {
			group.PromotionState = "identity_conflict"
			continue
		}
		sources := make([]releasegroups.CatalogSource, 0, len(group.Sources))
		for _, source := range group.Sources {
			if source.Provider == "lastfm" {
				continue
			}
			sources = append(sources, releasegroups.CatalogSource{
				Provider: source.Provider, Namespace: source.Namespace, ID: source.ID,
				Title: source.Title, Date: source.Date, Kind: source.Kind, URL: source.URL,
				ArtistName: source.ArtistName, ObservationID: source.ObservationID,
				ObservedAt: source.ObservedAt, TrackCount: metadataInt(source.Metadata, "track_count"),
			})
		}
		result, err := service.PromoteCatalogCluster(ctx, artistID, sources)
		if err != nil {
			group.PromotionState, group.PromotionError = "promotion_failed", err.Error()
			continue
		}
		_, _ = runtime.DB.Exec(ctx, `INSERT INTO artist_catalog_promotions(artist_entity_id,release_group_entity_id,state)VALUES($1,$2,'active')ON CONFLICT(artist_entity_id,release_group_entity_id)DO UPDATE SET state='active',updated_at=now()`, artistID, result.EntityID)
		if len(targets) == 1 {
			group.PromotionState = "canonical"
		} else {
			group.PromotionState = "promoted"
		}
	}
}

func hasProvider(group cluster, provider string) bool {
	for _, source := range group.Sources {
		if source.Provider == provider {
			return true
		}
	}
	return false
}

func authoritativeProviders(group cluster) map[string]bool {
	result := map[string]bool{}
	for _, source := range group.Sources {
		if source.Provider != "lastfm" {
			result[source.Provider] = true
		}
	}
	return result
}

func metadataInt(metadata map[string]any, key string) int {
	switch value := metadata[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	}
	return 0
}

func clusterTargets(ctx context.Context, runtime *platform.Runtime, group cluster) (map[string]bool, error) {
	targets := map[string]bool{}
	for _, source := range group.Sources {
		var target string
		err := runtime.DB.QueryRow(ctx, `SELECT entity_id::text FROM external_id_claims WHERE entity_kind='release_group' AND provider=$1 AND namespace=$2 AND normalized_value=$3 AND state='accepted'`, source.Provider, source.Namespace, source.ID).Scan(&target)
		if err != nil && err != pgx.ErrNoRows {
			return nil, err
		}
		if target != "" {
			targets[target] = true
		}
	}
	return targets, nil
}

func clusterConfidence(group cluster) float64 {
	if group.PromotionState == "identity_conflict" || group.PromotionState == "promotion_failed" {
		return 0.4
	}
	if hasProvider(group, "musicbrainz") {
		return 1
	}
	switch len(authoritativeProviders(group)) {
	case 0:
		return 0.4
	case 1:
		return 0.72
	case 2:
		return 0.92
	default:
		return 0.97
	}
}

func anchoredCluster(group cluster) bool {
	return hasProvider(group, "musicbrainz") || hasProvider(group, "discogs")
}

func reconcilePromotions(ctx context.Context, runtime *platform.Runtime, artistID string) error {
	_, err := runtime.DB.Exec(ctx, `
		UPDATE artist_catalog_promotions promotion SET state='active',updated_at=now()
		WHERE promotion.artist_entity_id=$1 AND EXISTS(
			SELECT 1 FROM entity_relations relation
			WHERE relation.source_entity_id=promotion.artist_entity_id
			  AND relation.target_entity_id=promotion.release_group_entity_id
			  AND relation.relation_type='discography' AND relation.state='accepted')`, artistID)
	if err != nil {
		return err
	}
	_, err = runtime.DB.Exec(ctx, `
		WITH orphaned AS (
			SELECT promotion.release_group_entity_id id
			FROM artist_catalog_promotions promotion
			WHERE promotion.artist_entity_id=$1 AND promotion.state='superseded'
			  AND NOT EXISTS(SELECT 1 FROM entity_relations relation WHERE relation.target_entity_id=promotion.release_group_entity_id AND relation.relation_type='discography' AND relation.state='accepted')
		)
		UPDATE entities SET deleted_at=now() WHERE id IN(SELECT id FROM orphaned)
		  AND NOT EXISTS(SELECT 1 FROM normalized_records record WHERE record.entity_id=entities.id AND record.normalizer_version NOT LIKE 'catalog-cluster-%')`, artistID)
	if err != nil {
		return err
	}
	_, err = runtime.DB.Exec(ctx, `DELETE FROM search_entities WHERE entity_id IN(SELECT release_group_entity_id FROM artist_catalog_promotions WHERE artist_entity_id=$1 AND state='superseded') AND NOT EXISTS(SELECT 1 FROM entity_relations relation WHERE relation.target_entity_id=search_entities.entity_id AND relation.relation_type='discography' AND relation.state='accepted')`, artistID)
	return err
}
func providerRank(v string) int {
	switch v {
	case "musicbrainz":
		return 1
	case "discogs":
		return 2
	case "apple":
		return 3
	case "deezer":
		return 4
	case "lastfm":
		return 5
	}
	return 9
}
func year(v string) int {
	if len(v) < 4 {
		return 0
	}
	n, _ := strconv.Atoi(v[:4])
	return n
}
func yearString(v int) string {
	if v < 1 {
		return ""
	}
	return strconv.Itoa(v)
}

func persistClusters(ctx context.Context, runtime *platform.Runtime, artistID string, clusters []cluster) error {
	seen := time.Now().UTC()
	if _, err := runtime.DB.Exec(ctx, `UPDATE entity_relations SET state='superseded' WHERE source_entity_id=$1 AND relation_type IN('discography','discography_candidate') AND state='accepted'`, artistID); err != nil {
		return err
	}
	for _, group := range clusters {
		rep := group.Sources[0]
		relationType := "discography_candidate"
		if anchoredCluster(group) {
			relationType = "discography"
		}
		sources := make([]map[string]any, 0, len(group.Sources))
		targetIDs := map[string]bool{}
		for _, s := range group.Sources {
			sources = append(sources, map[string]any{"provider": s.Provider, "namespace": s.Namespace, "value": s.ID, "title": s.Title, "date": s.Date, "kind": s.Kind, "url": s.URL, "metadata": s.Metadata, "match_reason": s.MatchReason, "match_confidence": s.MatchConfidence})
			var target string
			_ = runtime.DB.QueryRow(ctx, `SELECT entity_id::text FROM external_id_claims WHERE entity_kind='release_group' AND provider=$1 AND namespace=$2 AND normalized_value=$3 AND state='accepted'`, s.Provider, s.Namespace, s.ID).Scan(&target)
			if target != "" {
				targetIDs[target] = true
			}
		}
		metadata, _ := json.Marshal(map[string]any{"title": rep.Title, "first_release_date": rep.Date, "primary_type": rep.Kind, "sources": sources, "source_count": len(sources), "confidence": clusterConfidence(group), "resolution_state": group.PromotionState, "promotion_error": group.PromotionError, "bridge_reason": group.BridgeReason, "bridge_confidence": group.BridgeConfidence})
		var target any
		if len(targetIDs) == 1 {
			for id := range targetIDs {
				target = id
			}
		}
		_, err := runtime.DB.Exec(ctx, `INSERT INTO entity_relations(source_entity_id,target_entity_id,source_kind,target_kind,relation_type,provider,namespace,provider_value,metadata,state,source_observation_id,first_observed_at,last_observed_at)VALUES($1,$2,'artist','release_group',$3,$4,$5,$6,$7,'accepted',NULLIF($8,'')::uuid,$9,$9)ON CONFLICT(source_entity_id,relation_type,provider,namespace,provider_value)DO UPDATE SET target_entity_id=EXCLUDED.target_entity_id,metadata=EXCLUDED.metadata,state='accepted',source_observation_id=EXCLUDED.source_observation_id,last_observed_at=EXCLUDED.last_observed_at`, artistID, target, relationType, rep.Provider, rep.Namespace, rep.ID, metadata, rep.ObservationID, seen)
		if err != nil {
			return fmt.Errorf("persist mixed discography: %w", err)
		}
	}
	return nil
}
