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

	"github.com/HeyaMedia/HeyaMetadata/internal/musiccredits"
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
)

const SyncVersion = "mixed-artist-catalog/v19"

// MusicBrainz uses this synthetic artist for compilation credits. Its browse
// catalog is effectively unbounded and is not a meaningful artist discography.
const variousArtistsMusicBrainzID = "89ad4ac3-39f7-470e-963a-56509c546377"

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
	TargetID         string
	AlternateTargets []string
}
type Result struct {
	ReleaseGroups  []ReleaseGroup
	Pages          int
	Candidates     int
	Gated          int
	Clusters       int
	PublicClusters int
}

func SyncArtist(ctx context.Context, runtime *platform.Runtime, artistEntityID, mbid string, jobID int64, releaseEvidence ...ReleaseEvidence) (result Result, returnErr error) {
	mbid = strings.ToLower(strings.TrimSpace(mbid))
	if _, err := runtime.DB.Exec(ctx, `INSERT INTO artist_catalog_sync_runs(river_job_id,artist_entity_id,musicbrainz_id,sync_version,state)VALUES($1,$2,$3,$4,'working')ON CONFLICT(river_job_id)DO UPDATE SET sync_version=EXCLUDED.sync_version,state='working',pages=0,release_groups=0,error=NULL,completed_at=NULL`, jobID, artistEntityID, mbid, SyncVersion); err != nil {
		return result, err
	}
	defer func() {
		if returnErr != nil {
			_, _ = runtime.DB.Exec(context.WithoutCancel(ctx), `UPDATE artist_catalog_sync_runs SET state='failed',error=$2,pages=$3,release_groups=$4,completed_at=now()WHERE river_job_id=$1`, jobID, returnErr.Error(), result.Pages, result.Clusters)
		}
	}()
	if artistCatalogExcluded(mbid) {
		if _, err := runtime.DB.Exec(ctx, `UPDATE entity_relations SET state='superseded',last_observed_at=now() WHERE source_entity_id=$1 AND source_kind='artist' AND relation_type='discography'`, artistEntityID); err != nil {
			return result, err
		}
		if _, err := runtime.DB.Exec(ctx, `UPDATE artist_catalog_promotions SET state='superseded',updated_at=now() WHERE artist_entity_id=$1`, artistEntityID); err != nil {
			return result, err
		}
		_, err := runtime.DB.Exec(ctx, `UPDATE artist_catalog_sync_runs SET state='completed',pages=0,release_groups=0,error=NULL,completed_at=now() WHERE river_job_id=$1`, jobID)
		return result, err
	}

	aliases, claims, err := artistContext(ctx, runtime, artistEntityID)
	if err != nil {
		return result, err
	}
	sets := map[string]map[string][]candidate{}
	mb := []candidate{}
	if mbid != "" {
		var pages int
		mb, pages, err = collectMusicBrainz(ctx, runtime, mbid, jobID)
		if err != nil {
			return result, err
		}
		result.Pages = pages
		sets["musicbrainz"] = map[string][]candidate{mbid: mb}
	}
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
	if mbid != "" && runtime.Config.Providers.LastFM.APIKey != "" {
		sets["lastfm"], err = collectLastFM(ctx, runtime, mbid, jobID)
		if err != nil {
			return result, err
		}
	}
	if err := appendExactReleaseEvidence(ctx, runtime, sets, aliases, claims, releaseEvidence, jobID); err != nil {
		return result, err
	}

	directRoots, err := directArtistCatalogRoots(ctx, runtime, artistEntityID)
	if err != nil {
		return result, err
	}
	selected := selectProviderIdentities(sets, aliases, directRoots)
	selected, result.Gated = gateSelectedStorefronts(selected, directRoots)
	all := append([]candidate(nil), mb...)
	for _, provider := range []string{"discogs", "apple", "deezer", "lastfm"} {
		all = append(all, selected[provider]...)
	}
	result.Candidates = len(all)
	clusters := clusterCandidates(all)
	clusters = enrichClustersWithDetailEvidence(ctx, runtime, clusters, jobID)
	_, _ = runtime.DB.Exec(ctx, `UPDATE artist_catalog_promotions SET state='superseded',updated_at=now() WHERE artist_entity_id=$1`, artistEntityID)
	promoteProviderOnlyClusters(ctx, runtime, artistEntityID, clusters)
	clusters, err = coalesceClustersByTarget(ctx, runtime, clusters)
	if err != nil {
		return result, err
	}
	clusters, err = coalesceClustersByIssuedTracks(ctx, runtime, clusters)
	if err != nil {
		return result, err
	}
	result.Clusters = len(clusters)
	for _, group := range clusters {
		if publicDiscographyCluster(group) {
			result.PublicClusters++
		}
	}
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

func appendExactReleaseEvidence(ctx context.Context, runtime *platform.Runtime, sets map[string]map[string][]candidate, aliases []string, claims map[string][]string, values []ReleaseEvidence, jobID int64) error {
	seen := map[string]bool{}
	for _, value := range values {
		value.Provider = strings.ToLower(strings.TrimSpace(value.Provider))
		value.Namespace = strings.ToLower(strings.TrimSpace(value.Namespace))
		value.ID = strings.TrimSpace(value.ID)
		key := value.Provider + "\x00" + value.Namespace + "\x00" + value.ID
		if value.ID == "" || seen[key] {
			continue
		}
		seen[key] = true
		var item candidate
		var rootID string
		var err error
		switch {
		case value.Provider == "apple" && value.Namespace == "album":
			item, rootID, err = collectExactAppleRelease(ctx, runtime, value.ID, aliases, claims["apple"], jobID)
		case value.Provider == "deezer" && value.Namespace == "album":
			item, rootID, err = collectExactDeezerRelease(ctx, runtime, value.ID, aliases, claims["deezer"], jobID)
		default:
			continue
		}
		if err != nil {
			return err
		}
		if rootID == "" || item.ID == "" {
			continue
		}
		if sets[value.Provider] == nil {
			sets[value.Provider] = map[string][]candidate{}
		}
		if !candidateExists(sets[value.Provider][rootID], item) {
			sets[value.Provider][rootID] = append(sets[value.Provider][rootID], item)
		}
	}
	return nil
}

func collectExactAppleRelease(ctx context.Context, runtime *platform.Runtime, albumID string, aliases, artistIDs []string, jobID int64) (candidate, string, error) {
	if len(artistIDs) == 0 {
		return candidate{}, "", nil
	}
	base := apple.New(runtime.Config.Providers.Apple)
	r, err := resolver(runtime, base.Capability(), jobID)
	if err != nil {
		return candidate{}, "", err
	}
	client := apple.NewCached(runtime.Config.Providers.Apple, r, "")
	payload, err := client.CollectITunesAlbum(ctx, albumID)
	if err != nil {
		return candidate{}, "", err
	}
	if payload.StatusCode == http.StatusNotFound {
		return candidate{}, "", nil
	}
	if payload.StatusCode != http.StatusOK {
		return candidate{}, "", &providers.StatusError{Provider: "apple", StatusCode: payload.StatusCode}
	}
	var envelope struct {
		Results []struct {
			WrapperType    string `json:"wrapperType"`
			CollectionID   int64  `json:"collectionId"`
			CollectionName string `json:"collectionName"`
			CollectionType string `json:"collectionType"`
			ArtistID       int64  `json:"artistId"`
			ArtistName     string `json:"artistName"`
			ReleaseDate    string `json:"releaseDate"`
			TrackCount     int    `json:"trackCount"`
			URL            string `json:"collectionViewUrl"`
		} `json:"results"`
	}
	if err := json.Unmarshal(payload.Body, &envelope); err != nil {
		return candidate{}, "", fmt.Errorf("decode exact Apple album evidence: %w", err)
	}
	for _, value := range envelope.Results {
		if !strings.EqualFold(value.WrapperType, "collection") || strconv.FormatInt(value.CollectionID, 10) != albumID || strings.TrimSpace(value.CollectionName) == "" {
			continue
		}
		rootID := creditedArtistRoot(strconv.FormatInt(value.ArtistID, 10), value.ArtistName, artistIDs, aliases)
		if rootID == "" {
			return candidate{}, "", nil
		}
		title, kind := storefrontTitle(value.CollectionName, value.CollectionType, value.TrackCount)
		return candidate{Provider: "apple", Namespace: "album", ID: albumID, Title: title, Date: value.ReleaseDate, Kind: kind, ArtistName: value.ArtistName, URL: value.URL, ObservationID: payload.ObservationID, ObservedAt: payload.ObservedAt, Metadata: map[string]any{"track_count": value.TrackCount, "storefront_title": value.CollectionName, "supplied_identifier": true}}, rootID, nil
	}
	return candidate{}, "", nil
}

func collectExactDeezerRelease(ctx context.Context, runtime *platform.Runtime, albumID string, aliases, artistIDs []string, jobID int64) (candidate, string, error) {
	if len(artistIDs) == 0 {
		return candidate{}, "", nil
	}
	base := deezer.New(runtime.Config.Providers.Deezer)
	r, err := resolver(runtime, base.Capability(), jobID)
	if err != nil {
		return candidate{}, "", err
	}
	client := deezer.NewCached(runtime.Config.Providers.Deezer, r)
	payloads, err := client.Collect(ctx, providers.Identifier{Provider: "deezer", Namespace: "album", Value: albumID})
	if err != nil {
		return candidate{}, "", err
	}
	if len(payloads) == 0 || payloads[0].StatusCode == http.StatusNotFound {
		return candidate{}, "", nil
	}
	payload := payloads[0]
	if payload.StatusCode != http.StatusOK {
		return candidate{}, "", &providers.StatusError{Provider: "deezer", StatusCode: payload.StatusCode}
	}
	var value struct {
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
		Contributors []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"contributors"`
	}
	if err := json.Unmarshal(payload.Body, &value); err != nil {
		return candidate{}, "", fmt.Errorf("decode exact Deezer album evidence: %w", err)
	}
	if strconv.FormatInt(value.ID, 10) != albumID || strings.TrimSpace(value.Title) == "" {
		return candidate{}, "", nil
	}
	creditIDs := []string{strconv.FormatInt(value.Artist.ID, 10)}
	creditNames := []string{value.Artist.Name}
	for _, contributor := range value.Contributors {
		creditIDs = append(creditIDs, strconv.FormatInt(contributor.ID, 10))
		creditNames = append(creditNames, contributor.Name)
	}
	rootID := ""
	for index, creditID := range creditIDs {
		name := ""
		if index < len(creditNames) {
			name = creditNames[index]
		}
		if rootID = creditedArtistRoot(creditID, name, artistIDs, aliases); rootID != "" {
			break
		}
	}
	if rootID == "" {
		return candidate{}, "", nil
	}
	return candidate{Provider: "deezer", Namespace: "album", ID: albumID, Title: value.Title, Date: value.ReleaseDate, Kind: normalizeKind(value.RecordType), ArtistName: strings.Join(cleanCreditNames(creditNames), " & "), URL: value.Link, ObservationID: payload.ObservationID, ObservedAt: payload.ObservedAt, Metadata: map[string]any{"track_count": value.TrackCount, "supplied_identifier": true}}, rootID, nil
}

func creditedArtistRoot(providerArtistID, credit string, artistIDs, aliases []string) string {
	for _, artistID := range artistIDs {
		if providerArtistID != "" && providerArtistID != "0" && providerArtistID == artistID {
			return artistID
		}
	}
	if len(artistIDs) == 1 && musiccredits.ContainsName(credit, aliases, equivalentArtistCredit) {
		return artistIDs[0]
	}
	return ""
}

func cleanCreditNames(values []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value != "" && !seen[key] {
			seen[key] = true
			result = append(result, value)
		}
	}
	return result
}

func candidateExists(values []candidate, wanted candidate) bool {
	for _, value := range values {
		if value.Provider == wanted.Provider && value.Namespace == wanted.Namespace && value.ID == wanted.ID {
			return true
		}
	}
	return false
}

func artistCatalogExcluded(mbid string) bool {
	return strings.EqualFold(strings.TrimSpace(mbid), variousArtistsMusicBrainzID)
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
		artistNames := []string{}
		for _, v := range body.Results {
			if strings.EqualFold(v.WrapperType, "artist") && strconv.FormatInt(v.ArtistID, 10) == id && strings.TrimSpace(v.ArtistName) != "" {
				artistNames = append(artistNames, v.ArtistName)
			}
		}
		for _, v := range body.Results {
			if !strings.EqualFold(v.WrapperType, "collection") || v.CollectionID < 1 || v.CollectionName == "" {
				continue
			}
			direct := strconv.FormatInt(v.ArtistID, 10) == id
			credited := musiccredits.ContainsName(v.ArtistName, artistNames, equivalentArtistCredit)
			if !direct && !credited {
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

func selectProviderIdentities(sets map[string]map[string][]candidate, aliases []string, directRoots map[string]string) map[string][]candidate {
	selected := map[string][]candidate{"musicbrainz": firstSet(sets["musicbrainz"])}
	anchors := selected["musicbrainz"]
	anchorProvider := "musicbrainz"
	if len(anchors) == 0 {
		anchorProvider = ""
		for _, provider := range []string{"apple", "deezer"} {
			rootID := directRoots[provider]
			if rootID == "" || len(sets[provider][rootID]) == 0 {
				continue
			}
			selected[provider] = sets[provider][rootID]
			anchors = selected[provider]
			anchorProvider = provider
			break
		}
	}
	if len(anchors) == 0 {
		return selected
	}
	// Trust is directional. Two same-name storefront catalogs are not allowed
	// to validate each other without first overlapping an anchored catalog.
	for _, p := range []string{"discogs", "apple", "deezer", "lastfm"} {
		if p == anchorProvider {
			continue
		}
		choices := sets[p]
		if len(choices) == 0 {
			continue
		}
		if rootID := directRoots[p]; rootID != "" && len(choices[rootID]) > 0 {
			selected[p] = choices[rootID]
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
		if musiccredits.ContainsName(stripDiscogsSuffix(v.ArtistName), aliases, equivalentArtistCredit) {
			return true
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

// gateSelectedStorefronts protects an otherwise valid Apple or Deezer artist
// page from leaking releases belonging to same-name artists. A plausibly sized
// page with meaningful MusicBrainz overlap is kept whole so genuinely digital-
// only releases survive. A wildly larger page is reduced to independently
// anchored releases; the exact provider response remains retained evidence.
func gateSelectedStorefronts(selected map[string][]candidate, directRoots map[string]string) (map[string][]candidate, int) {
	anchors := selected["musicbrainz"]
	anchorProvider := "musicbrainz"
	if len(anchors) == 0 {
		anchorProvider = ""
		for _, provider := range []string{"apple", "deezer"} {
			if directRoots[provider] != "" && len(selected[provider]) > 0 {
				anchors = selected[provider]
				anchorProvider = provider
				for index := range selected[provider] {
					if selected[provider][index].Metadata == nil {
						selected[provider][index].Metadata = map[string]any{}
					}
					selected[provider][index].Metadata["catalog_identity_gate"] = "canonical_artist_provider_root"
				}
				break
			}
		}
	}
	dropped := 0
	for _, provider := range []string{"apple", "deezer"} {
		if directRoots[provider] != "" && len(selected[provider]) > 0 {
			for index := range selected[provider] {
				if selected[provider][index].Metadata == nil {
					selected[provider][index].Metadata = map[string]any{}
				}
				selected[provider][index].Metadata["catalog_identity_gate"] = "canonical_artist_provider_root"
			}
			continue
		}
		if provider == anchorProvider {
			continue
		}
		plausible := storefrontCatalogPlausible(selected[provider], anchors)
		kept, n := gateStorefrontCandidates(selected[provider], anchors)
		if plausible {
			for i := range kept {
				if kept[i].Metadata == nil {
					kept[i].Metadata = map[string]any{}
				}
				kept[i].Metadata["catalog_identity_gate"] = "musicbrainz_overlap_plausible_page"
			}
		}
		selected[provider] = kept
		dropped += n
	}
	return selected, dropped
}

func gateStorefrontCandidates(values, anchors []candidate) ([]candidate, int) {
	if len(values) == 0 || len(anchors) == 0 {
		return values, 0
	}
	if storefrontCatalogPlausible(values, anchors) {
		return values, 0
	}
	overlapping := overlap(values, anchors)
	kept := make([]candidate, 0, overlapping)
	for _, value := range values {
		for _, anchor := range anchors {
			if compatible(value, anchor) || exactDateCrossScript(value, anchor) {
				kept = append(kept, value)
				break
			}
		}
	}
	return kept, len(values) - len(kept)
}

func storefrontCatalogPlausible(values, anchors []candidate) bool {
	if len(values) == 0 || len(anchors) == 0 {
		return false
	}
	overlapping := overlap(values, anchors)
	return overlapping > 0 && len(values) <= 2*max(overlapping, len(anchors))+5
}

func clusterCandidates(values []candidate) []cluster {
	sort.SliceStable(values, func(i, j int) bool { return providerRank(values[i].Provider) < providerRank(values[j].Provider) })
	out := []cluster{}
	for _, v := range values {
		type match struct {
			index      int
			reason     string
			confidence float64
		}
		matches := []match{}
		for i, c := range out {
			if ok, reason, confidence := candidateMatch(v, c.Sources[0]); ok {
				matches = append(matches, match{index: i, reason: reason, confidence: confidence})
			}
		}
		matched := -1
		matchReason := ""
		matchConfidence := 0.0
		if len(matches) > 0 {
			sort.SliceStable(matches, func(i, j int) bool { return matches[i].confidence > matches[j].confidence })
			// Equal evidence for several canonical groups is ambiguity, not
			// permission to attach to whichever happened to sort first.
			if len(matches) == 1 || matches[0].confidence > matches[1].confidence {
				matched = matches[0].index
				matchReason, matchConfidence = matches[0].reason, matches[0].confidence
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
	if a.Provider == b.Provider {
		if a.Namespace == b.Namespace && a.ID != "" && a.ID == b.ID {
			return true, "provider_identifier", 1
		}
		if a.Provider == "discogs" && discogsMasterReleasePair(a, b) {
			return true, "discogs_master_main_release", 0.99
		}
		// Separate provider records are separate identities unless the
		// provider itself supplied an explicit relationship above.
		return false, "", 0
	}
	if textmatch.EquivalentRelease(a.Title, ay, b.Title, by) {
		if ay > 0 && by > 0 && ay == by {
			return true, "normalized_title_type_year", 0.96
		}
		return true, "normalized_title_type", 0.91
	}
	return false, "", 0
}

func discogsMasterReleasePair(a, b candidate) bool {
	if a.Namespace == "master" && b.Namespace == "release" {
		return metadataInt(a.Metadata, "main_release") > 0 && strconv.Itoa(metadataInt(a.Metadata, "main_release")) == b.ID
	}
	if b.Namespace == "master" && a.Namespace == "release" {
		return metadataInt(b.Metadata, "main_release") > 0 && strconv.Itoa(metadataInt(b.Metadata, "main_release")) == a.ID
	}
	return false
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
		targets, err := clusterTargets(ctx, runtime, *group)
		if err != nil {
			group.PromotionState, group.PromotionError = "promotion_failed", err.Error()
			continue
		}
		if len(targets) > 1 {
			group.PromotionState = "identity_conflict"
			continue
		}
		if soleIndependentTarget(targets) {
			group.PromotionState = "canonical"
			continue
		}
		if !promotableProviderCluster(*group) {
			group.PromotionState = "unresolved_single_provider"
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
		group.PromotionState = "promoted"
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

func promotableProviderCluster(group cluster) bool {
	return len(authoritativeProviders(group)) >= 2 || identityGatedStorefrontCluster(group)
}

func identityGatedStorefrontCluster(group cluster) bool {
	for _, source := range group.Sources {
		if source.Provider != "apple" && source.Provider != "deezer" {
			continue
		}
		if source.Metadata["catalog_identity_gate"] == "musicbrainz_overlap_plausible_page" || source.Metadata["catalog_identity_gate"] == "canonical_artist_provider_root" {
			return true
		}
	}
	return false
}

func directArtistCatalogRoots(ctx context.Context, runtime *platform.Runtime, artistEntityID string) (map[string]string, error) {
	rows, err := runtime.DB.Query(ctx, `
		SELECT DISTINCT record.provider,record.provider_record_id
		FROM normalized_records record
		JOIN external_id_claims claim
		  ON claim.entity_id=record.entity_id AND claim.entity_kind='artist'
		 AND claim.provider=record.provider AND claim.namespace=record.provider_namespace
		 AND claim.normalized_value=record.provider_record_id AND claim.state='accepted'
		WHERE record.entity_id=$1 AND record.entity_kind='artist'
		  AND record.provider IN('apple','deezer') AND record.provider_namespace='artist'
		ORDER BY record.provider,record.provider_record_id`, artistEntityID)
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
		if result[provider] != "" && result[provider] != value {
			return nil, fmt.Errorf("artist %s has multiple direct %s roots", artistEntityID, provider)
		}
		result[provider] = value
	}
	return result, rows.Err()
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
	type sourceID struct {
		Provider  string `json:"provider"`
		Namespace string `json:"namespace"`
		Value     string `json:"value"`
	}
	sources := make([]sourceID, 0, len(group.Sources))
	for _, source := range group.Sources {
		if source.Provider != "" && source.Namespace != "" && source.ID != "" {
			sources = append(sources, sourceID{Provider: source.Provider, Namespace: source.Namespace, Value: source.ID})
		}
	}
	if len(sources) == 0 {
		return targets, nil
	}
	raw, err := json.Marshal(sources)
	if err != nil {
		return nil, fmt.Errorf("encode catalog source identities: %w", err)
	}
	rows, err := runtime.DB.Query(ctx, `
		SELECT claim.entity_id::text,
		       EXISTS(SELECT 1 FROM normalized_records record WHERE record.entity_id=claim.entity_id AND record.normalizer_version NOT LIKE 'catalog-cluster-%') independent
		FROM jsonb_to_recordset($1::jsonb) source(provider text, namespace text, value text)
		JOIN external_id_claims claim
		  ON claim.entity_kind='release_group' AND claim.state='accepted'
		 AND claim.provider=source.provider AND claim.namespace=source.namespace AND claim.normalized_value=source.value
		JOIN entities entity ON entity.id=claim.entity_id AND entity.deleted_at IS NULL
		GROUP BY claim.entity_id`, string(raw))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var target string
		var independent bool
		if err := rows.Scan(&target, &independent); err != nil {
			return nil, err
		}
		targets[target] = targets[target] || independent
	}
	return targets, rows.Err()
}

func soleIndependentTarget(targets map[string]bool) bool {
	if len(targets) != 1 {
		return false
	}
	for _, independent := range targets {
		return independent
	}
	return false
}

func clusterConfidence(group cluster) float64 {
	if group.PromotionState == "identity_conflict" || group.PromotionState == "promotion_failed" {
		return 0.4
	}
	if hasProvider(group, "musicbrainz") {
		return 1
	}
	if group.PromotionState == "canonical" {
		return .99
	}
	switch len(authoritativeProviders(group)) {
	case 0:
		return 0.4
	case 1:
		if identityGatedStorefrontCluster(group) {
			return .86
		}
		return 0.72
	case 2:
		return 0.92
	default:
		return 0.97
	}
}

func publicDiscographyCluster(group cluster) bool {
	if canonicalSpineCluster(group) {
		return true
	}
	return group.PromotionState == "canonical" || group.PromotionState == "promoted"
}

// MusicBrainz release groups are the initial canonical spine. Other provider
// records may enrich that spine or be promoted after independent resolution,
// but they must not become anchors merely because they were returned upstream.
func canonicalSpineCluster(group cluster) bool {
	return hasProvider(group, "musicbrainz")
}

// coalesceClustersByTarget removes duplicate presentation rows when different
// provider evidence converges on the same canonical release group. Canonical
// entity merging remains a separate, conflict-aware operation.
func coalesceClustersByTarget(ctx context.Context, runtime *platform.Runtime, clusters []cluster) ([]cluster, error) {
	for i := range clusters {
		group := &clusters[i]
		targets, err := clusterTargets(ctx, runtime, *group)
		if err != nil {
			return nil, err
		}
		if len(targets) == 1 {
			for id := range targets {
				group.TargetID = id
			}
		} else if evidenceBackedBridge(*group) {
			if target, alternates, ok := preferredIndependentTarget(targets); ok {
				group.TargetID = target
				for _, alternate := range alternates {
					group.AlternateTargets = appendUnique(group.AlternateTargets, alternate)
				}
			}
		}
	}
	return coalesceTargetedClusters(clusters), nil
}

func evidenceBackedBridge(group cluster) bool {
	switch group.BridgeReason {
	case "shared_barcode", "shared_isrc_trackset", "ordered_tracklist_duration", "chromaprint_recording_coverage", "issued_release_track_overlap":
		return true
	default:
		return false
	}
}

func preferredIndependentTarget(targets map[string]bool) (string, []string, bool) {
	preferred := ""
	alternates := []string{}
	for target, independent := range targets {
		if independent {
			if preferred != "" {
				return "", nil, false
			}
			preferred = target
			continue
		}
		alternates = append(alternates, target)
	}
	sort.Strings(alternates)
	return preferred, alternates, preferred != ""
}

func coalesceTargetedClusters(clusters []cluster) []cluster {
	result := make([]cluster, 0, len(clusters))
	byTarget := map[string]int{}
	for _, group := range clusters {
		if group.TargetID == "" {
			result = append(result, group)
			continue
		}
		index, exists := byTarget[group.TargetID]
		if !exists {
			byTarget[group.TargetID] = len(result)
			result = append(result, group)
			continue
		}
		for _, source := range group.Sources {
			if !hasSource(result[index], source) {
				source.MatchReason = "canonical_target"
				source.MatchConfidence = 1
				result[index].Sources = append(result[index].Sources, source)
			}
		}
		result[index].BridgeReason = "canonical_target"
		result[index].BridgeConfidence = 1
		if canonicalSpineCluster(group) {
			result[index].PromotionState = "musicbrainz_spine"
		}
	}
	return result
}

type issuedTrack struct {
	Title      string
	DurationMS int64
}

type issuedTrackEvidence map[string][][]issuedTrack

func coalesceClustersByIssuedTracks(ctx context.Context, runtime *platform.Runtime, clusters []cluster) ([]cluster, error) {
	targets := make([]string, 0, len(clusters))
	for _, group := range clusters {
		if group.TargetID != "" {
			targets = append(targets, group.TargetID)
		}
	}
	if len(targets) < 2 {
		return clusters, nil
	}
	rows, err := runtime.DB.Query(ctx, `
		SELECT relation.source_entity_id::text, relation.target_entity_id::text, track.title, COALESCE(track.duration_ms,0)
		FROM entity_relations relation
		JOIN release_tracks track ON track.release_entity_id=relation.target_entity_id
		WHERE relation.source_entity_id=ANY($1::uuid[])
		  AND relation.relation_type='editions' AND relation.state='accepted'
		ORDER BY relation.source_entity_id, relation.target_entity_id, track.sequence`, targets)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	evidence := issuedTrackEvidence{}
	releaseIndexes := map[string]map[string]int{}
	for rows.Next() {
		var groupID, releaseID, title string
		var duration int64
		if err := rows.Scan(&groupID, &releaseID, &title, &duration); err != nil {
			return nil, err
		}
		if releaseIndexes[groupID] == nil {
			releaseIndexes[groupID] = map[string]int{}
		}
		index, ok := releaseIndexes[groupID][releaseID]
		if !ok {
			index = len(evidence[groupID])
			releaseIndexes[groupID][releaseID] = index
			evidence[groupID] = append(evidence[groupID], []issuedTrack{})
		}
		evidence[groupID][index] = append(evidence[groupID][index], issuedTrack{Title: title, DurationMS: duration})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return coalesceClustersWithIssuedTrackEvidence(clusters, evidence), nil
}

func coalesceClustersWithIssuedTrackEvidence(clusters []cluster, evidence issuedTrackEvidence) []cluster {
	result := make([]cluster, 0, len(clusters))
	for _, group := range clusters {
		matched := -1
		for i := range result {
			if releaseConceptCandidates(result[i], group) && issuedReleasesOverlap(evidence[result[i].TargetID], evidence[group.TargetID]) {
				matched = i
				break
			}
		}
		if matched < 0 {
			result = append(result, group)
			continue
		}
		for _, source := range group.Sources {
			if !hasSource(result[matched], source) {
				source.MatchReason = "issued_release_track_overlap"
				source.MatchConfidence = .96
				result[matched].Sources = append(result[matched].Sources, source)
			}
		}
		result[matched].AlternateTargets = appendUnique(result[matched].AlternateTargets, group.TargetID)
		for _, target := range group.AlternateTargets {
			result[matched].AlternateTargets = appendUnique(result[matched].AlternateTargets, target)
		}
		result[matched].BridgeReason = "issued_release_track_overlap"
		result[matched].BridgeConfidence = .96
	}
	return result
}

func releaseConceptCandidates(a, b cluster) bool {
	if a.TargetID == "" || b.TargetID == "" || a.TargetID == b.TargetID || len(a.Sources) == 0 || len(b.Sources) == 0 {
		return false
	}
	left, right := a.Sources[0], b.Sources[0]
	leftYear, rightYear := year(left.Date), year(right.Date)
	if leftYear == 0 || leftYear != rightYear || left.Kind != "" && right.Kind != "" && left.Kind != right.Kind {
		return false
	}
	return textmatch.EquivalentRelease(left.Title, leftYear, right.Title, rightYear)
}

func issuedReleasesOverlap(left, right [][]issuedTrack) bool {
	for _, a := range left {
		for _, b := range right {
			minimum := min(len(a), len(b))
			if minimum < 2 {
				continue
			}
			used := map[int]bool{}
			matched := 0
			for _, x := range a {
				for j, y := range b {
					if used[j] || !textmatch.EquivalentRelease(x.Title, 0, y.Title, 0) || !durationClose(x.DurationMS, y.DurationMS) {
						continue
					}
					used[j] = true
					matched++
					break
				}
			}
			required := 3
			if minimum == 2 {
				required = 2
			}
			if matched >= required && matched*100/minimum >= 50 {
				return true
			}
		}
	}
	return false
}

func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func reconcilePromotions(ctx context.Context, runtime *platform.Runtime, artistID string) error {
	_, err := runtime.DB.Exec(ctx, `
		UPDATE artist_catalog_promotions promotion SET state='active',updated_at=now()
		WHERE promotion.artist_entity_id=$1 AND (
			EXISTS(
				SELECT 1 FROM entity_relations relation
				WHERE relation.source_entity_id=promotion.artist_entity_id
				  AND relation.target_entity_id=promotion.release_group_entity_id
				  AND relation.relation_type='discography' AND relation.state='accepted')
			OR EXISTS(
				SELECT 1 FROM entity_relations relation
				WHERE relation.source_entity_id=promotion.artist_entity_id
				  AND relation.relation_type='discography' AND relation.state='accepted'
				  AND COALESCE(relation.metadata->'alternate_target_entity_ids','[]'::jsonb) ? promotion.release_group_entity_id::text))`, artistID)
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
		if publicDiscographyCluster(group) {
			relationType = "discography"
		}
		sources := make([]map[string]any, 0, len(group.Sources))
		for _, s := range group.Sources {
			sources = append(sources, map[string]any{"provider": s.Provider, "namespace": s.Namespace, "value": s.ID, "title": s.Title, "date": s.Date, "kind": s.Kind, "url": s.URL, "metadata": s.Metadata, "match_reason": s.MatchReason, "match_confidence": s.MatchConfidence})
		}
		metadata, _ := json.Marshal(map[string]any{"title": rep.Title, "first_release_date": rep.Date, "primary_type": rep.Kind, "sources": sources, "source_count": len(sources), "confidence": clusterConfidence(group), "resolution_state": group.PromotionState, "promotion_error": group.PromotionError, "bridge_reason": group.BridgeReason, "bridge_confidence": group.BridgeConfidence, "alternate_target_entity_ids": group.AlternateTargets})
		var target any
		if group.TargetID != "" {
			target = group.TargetID
		}
		_, err := runtime.DB.Exec(ctx, `INSERT INTO entity_relations(source_entity_id,target_entity_id,source_kind,target_kind,relation_type,provider,namespace,provider_value,metadata,state,source_observation_id,first_observed_at,last_observed_at)VALUES($1,$2,'artist','release_group',$3,$4,$5,$6,$7,'accepted',NULLIF($8,'')::uuid,$9,$9)ON CONFLICT(source_entity_id,relation_type,provider,namespace,provider_value)DO UPDATE SET target_entity_id=EXCLUDED.target_entity_id,metadata=EXCLUDED.metadata,state='accepted',source_observation_id=EXCLUDED.source_observation_id,last_observed_at=EXCLUDED.last_observed_at`, artistID, target, relationType, rep.Provider, rep.Namespace, rep.ID, metadata, rep.ObservationID, seen)
		if err != nil {
			return fmt.Errorf("persist mixed discography: %w", err)
		}
	}
	return nil
}
