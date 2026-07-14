package musiccatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/musiccredits"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/textmatch"
)

// IdentityRelease is compact catalog evidence used only to reconcile an
// upstream artist identity. It deliberately excludes tracks and artwork: a
// provider catalog can corroborate an artist without becoming the public
// release projection itself.
type IdentityRelease struct {
	Provider  string
	Namespace string
	ID        string
	Title     string
	Date      string
	Kind      string
}

// ReleaseEvidence is an exact upstream release identifier supplied alongside
// artist discovery. It is fetched and attributed inside HeyaMetadata; callers
// never choose how it participates in canonical reconciliation.
type ReleaseEvidence struct {
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	ID        string `json:"id"`
}

// AppleIdentityCatalog extracts the albums belonging to one artist from an
// iTunes artist lookup response.
func AppleIdentityCatalog(body []byte, artistID string) ([]IdentityRelease, error) {
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
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode Apple artist catalog identity evidence: %w", err)
	}
	artistNames := []string{}
	for _, value := range envelope.Results {
		if strings.EqualFold(value.WrapperType, "artist") && strconv.FormatInt(value.ArtistID, 10) == artistID && strings.TrimSpace(value.ArtistName) != "" {
			artistNames = append(artistNames, value.ArtistName)
		}
	}
	result := make([]IdentityRelease, 0, len(envelope.Results))
	for _, value := range envelope.Results {
		if !strings.EqualFold(value.WrapperType, "collection") || value.CollectionID < 1 {
			continue
		}
		direct := strconv.FormatInt(value.ArtistID, 10) == artistID
		credited := musiccredits.ContainsName(value.ArtistName, artistNames, equivalentArtistCredit)
		if !direct && !credited {
			continue
		}
		title, kind := storefrontTitle(value.CollectionName, value.CollectionType, value.TrackCount)
		if title == "" {
			continue
		}
		result = append(result, IdentityRelease{Provider: "apple", Namespace: "album", ID: strconv.FormatInt(value.CollectionID, 10), Title: title, Date: value.ReleaseDate, Kind: kind})
	}
	return uniqueIdentityReleases(result), nil
}

// DeezerIdentityCatalog extracts albums from the provider's artist-scoped
// endpoint. The endpoint itself is the structured attribution; collaborative
// albums can legitimately name another primary artist in an individual row.
func DeezerIdentityCatalog(body []byte, artistID string) ([]IdentityRelease, int, error) {
	var envelope struct {
		Total int `json:"total"`
		Data  []struct {
			ID          int64  `json:"id"`
			Title       string `json:"title"`
			ReleaseDate string `json:"release_date"`
			RecordType  string `json:"record_type"`
			Artist      struct {
				ID int64 `json:"id"`
			} `json:"artist"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, 0, fmt.Errorf("decode Deezer artist catalog identity evidence: %w", err)
	}
	result := make([]IdentityRelease, 0, len(envelope.Data))
	for _, value := range envelope.Data {
		if value.ID < 1 || strings.TrimSpace(value.Title) == "" {
			continue
		}
		result = append(result, IdentityRelease{Provider: "deezer", Namespace: "album", ID: strconv.FormatInt(value.ID, 10), Title: value.Title, Date: value.ReleaseDate, Kind: normalizeKind(value.RecordType)})
	}
	return uniqueIdentityReleases(result), envelope.Total, nil
}

func equivalentArtistCredit(left, right string) bool {
	return textmatch.EquivalentRelease(left, 0, right, 0)
}

// IdentityCatalogOverlap counts a one-to-one set of compatible releases. It
// is intentionally conservative: titles must be equivalent, release types
// compatible, and any known years must agree.
func IdentityCatalogOverlap(left, right []IdentityRelease) int {
	used := map[int]bool{}
	matches := 0
	for _, a := range left {
		for index, b := range right {
			if used[index] || !identityReleaseCompatible(a, b) {
				continue
			}
			used[index] = true
			matches++
			break
		}
	}
	return matches
}

// FindCanonicalArtistByCatalog returns an existing exact-name artist only when
// its retained discography is the unique catalog match. Two matching releases
// are required so a ubiquitous single title cannot merge namesakes.
func FindCanonicalArtistByCatalog(ctx context.Context, runtime *platform.Runtime, artistName string, incoming []IdentityRelease) (string, int, error) {
	artistName = strings.TrimSpace(artistName)
	if artistName == "" || len(incoming) < 2 {
		return "", 0, nil
	}
	rows, err := runtime.DB.Query(ctx, `
		SELECT DISTINCT name.entity_id::text
		FROM search_names name
		JOIN entities entity ON entity.id=name.entity_id
		WHERE entity.kind='artist' AND entity.deleted_at IS NULL
		  AND lower(unaccent(name.value))=lower(unaccent($1))
		ORDER BY name.entity_id::text`, artistName)
	if err != nil {
		return "", 0, err
	}
	entityIDs := []string{}
	for rows.Next() {
		var entityID string
		if err := rows.Scan(&entityID); err != nil {
			rows.Close()
			return "", 0, err
		}
		entityIDs = append(entityIDs, entityID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return "", 0, err
	}
	rows.Close()
	if len(entityIDs) == 0 {
		return "", 0, nil
	}

	type score struct {
		entityID string
		overlap  int
	}
	scores := make([]score, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		catalog, err := retainedIdentityCatalog(ctx, runtime, entityID)
		if err != nil {
			return "", 0, err
		}
		scores = append(scores, score{entityID: entityID, overlap: IdentityCatalogOverlap(incoming, catalog)})
	}
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].overlap != scores[j].overlap {
			return scores[i].overlap > scores[j].overlap
		}
		return scores[i].entityID < scores[j].entityID
	})
	if len(scores) == 0 || scores[0].overlap < 2 || len(scores) > 1 && scores[0].overlap == scores[1].overlap {
		return "", 0, nil
	}
	return scores[0].entityID, scores[0].overlap, nil
}

func retainedIdentityCatalog(ctx context.Context, runtime *platform.Runtime, entityID string) ([]IdentityRelease, error) {
	rows, err := runtime.DB.Query(ctx, `
		SELECT provider,namespace,provider_value,metadata
		FROM entity_relations
		WHERE source_entity_id=$1 AND source_kind='artist'
		  AND relation_type IN('discography','discography_candidate')
		  AND state='accepted'`, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []IdentityRelease{}
	for rows.Next() {
		var provider, namespace, value string
		var raw []byte
		if err := rows.Scan(&provider, &namespace, &value, &raw); err != nil {
			return nil, err
		}
		var metadata struct {
			Title            string `json:"title"`
			FirstReleaseDate string `json:"first_release_date"`
			PrimaryType      string `json:"primary_type"`
			Sources          []struct {
				Provider  string `json:"provider"`
				Namespace string `json:"namespace"`
				Value     string `json:"value"`
				Title     string `json:"title"`
				Date      string `json:"date"`
				Kind      string `json:"kind"`
			} `json:"sources"`
		}
		if err := json.Unmarshal(raw, &metadata); err != nil {
			return nil, fmt.Errorf("decode retained artist catalog evidence: %w", err)
		}
		if len(metadata.Sources) == 0 && metadata.Title != "" {
			result = append(result, IdentityRelease{Provider: provider, Namespace: namespace, ID: value, Title: metadata.Title, Date: metadata.FirstReleaseDate, Kind: normalizeKind(metadata.PrimaryType)})
		}
		for _, source := range metadata.Sources {
			result = append(result, IdentityRelease{Provider: source.Provider, Namespace: source.Namespace, ID: source.Value, Title: source.Title, Date: source.Date, Kind: normalizeKind(source.Kind)})
		}
	}
	return uniqueIdentityReleases(result), rows.Err()
}

func identityReleaseCompatible(a, b IdentityRelease) bool {
	if a.Provider == b.Provider && a.Namespace == b.Namespace && a.ID != "" && a.ID == b.ID {
		return true
	}
	aKind, bKind := normalizeKind(a.Kind), normalizeKind(b.Kind)
	if aKind != "" && bKind != "" && aKind != bKind {
		return false
	}
	aYear, bYear := year(a.Date), year(b.Date)
	if aYear > 0 && bYear > 0 && aYear != bYear {
		return false
	}
	aTitle, _ := storefrontTitle(a.Title, a.Kind, 0)
	bTitle, _ := storefrontTitle(b.Title, b.Kind, 0)
	return textmatch.EquivalentRelease(aTitle, aYear, bTitle, bYear)
}

func uniqueIdentityReleases(values []IdentityRelease) []IdentityRelease {
	seen := map[string]bool{}
	result := make([]IdentityRelease, 0, len(values))
	for _, value := range values {
		value.Title = strings.TrimSpace(value.Title)
		if value.Title == "" {
			continue
		}
		key := value.Provider + "\x00" + value.Namespace + "\x00" + value.ID
		if value.ID == "" {
			key = strings.ToLower(value.Title) + "\x00" + value.Date + "\x00" + value.Kind
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}
