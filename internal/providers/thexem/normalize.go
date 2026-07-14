package thexem

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/episodic"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

const NormalizerVersion = "thexem-episode-mapping/v2"

type Address struct {
	Season   int     `json:"season"`
	Episode  float64 `json:"episode"`
	Absolute float64 `json:"absolute"`
}

type Row map[string]Address

type Mapping struct {
	TVDBID string
	Rows   []Row
}

// CanonicalAnimeSeason resolves the TVDB address used by TMDB's external
// identity into TheXEM's AniDB season partition. Anime Lists uses the same
// TVDB episode offsets, so this also places cour-specific identities on the
// season that owns their first episode.
func (m Mapping) CanonicalAnimeSeason(tvdbSeason, tvdbEpisode int) (int, bool) {
	for _, row := range m.Rows {
		tvdb, ok := row["tvdb"]
		if !ok || tvdb.Season != tvdbSeason || tvdb.Episode != float64(tvdbEpisode) {
			continue
		}
		canonical, ok := canonicalAnimeAddress(row)
		if ok {
			return canonical.Season, true
		}
	}
	return 0, false
}

func ParseMapping(payload providers.Payload, tvdbID string) (Mapping, error) {
	if payload.StatusCode != http.StatusOK {
		return Mapping{}, &providers.StatusError{Provider: "thexem", StatusCode: payload.StatusCode}
	}
	var envelope struct {
		Result string `json:"result"`
		Data   []Row  `json:"data"`
	}
	if err := json.Unmarshal(payload.Body, &envelope); err != nil {
		return Mapping{}, fmt.Errorf("decode TheXEM episode mapping: %w", err)
	}
	if !strings.EqualFold(envelope.Result, "success") {
		return Mapping{}, fmt.Errorf("TheXEM episode mapping was not successful")
	}
	mapping := Mapping{TVDBID: strings.TrimSpace(tvdbID)}
	for _, row := range envelope.Data {
		if tvdb, ok := row["tvdb"]; !ok || tvdb.Season < 0 || tvdb.Episode <= 0 {
			continue
		}
		mapping.Rows = append(mapping.Rows, row)
	}
	return mapping, nil
}

func NormalizeAnime(payloads []providers.Payload, tvdbID string) (episodic.NormalizedRecord, Mapping, error) {
	if len(payloads) == 0 {
		return episodic.NormalizedRecord{}, Mapping{}, fmt.Errorf("TheXEM returned no episode mapping")
	}
	mapping, err := ParseMapping(payloads[0], tvdbID)
	if err != nil {
		return episodic.NormalizedRecord{}, Mapping{}, err
	}
	record := episodic.NormalizedRecord{
		SchemaVersion: 1, Kind: "anime", Provider: "thexem", Namespace: "mapping",
		ProviderID: "tvdb:" + mapping.TVDBID, PrimaryObservationID: payloads[0].ObservationID,
		ObservedAt: payloads[0].ObservedAt, NormalizerVersion: NormalizerVersion,
	}
	if len(payloads) > 1 && payloads[1].StatusCode == http.StatusOK {
		record.Titles = normalizeNames(payloads[1].Body)
		record.Contributors = append(record.Contributors, episodic.Contributor{
			Provider: "thexem", ObservationID: payloads[1].ObservationID, NormalizerVersion: NormalizerVersion,
		})
	}

	seasonIndexes := map[int]int{}
	for _, row := range mapping.Rows {
		canonical, ok := canonicalAnimeAddress(row)
		if !ok || canonical.Episode <= 0 {
			continue
		}
		seasonNumber := canonical.Season
		if _, found := seasonIndexes[seasonNumber]; !found {
			name := "Specials"
			if seasonNumber > 0 {
				name = "Season " + strconv.Itoa(seasonNumber)
			}
			seasonIndexes[seasonNumber] = len(record.Seasons)
			record.Seasons = append(record.Seasons, episodic.Season{
				Number: seasonNumber, Name: name,
				Titles: []episodic.Title{{Value: name, Language: "en", Type: "display"}},
				ExternalIDs: []episodic.ExternalID{{
					Provider: "thexem", Namespace: "anime_season",
					Value: fmt.Sprintf("tvdb:%s:%d", mapping.TVDBID, seasonNumber),
				}},
			})
		}
		season := &record.Seasons[seasonIndexes[seasonNumber]]
		season.EpisodeCount++
		season.EpisodeOrder++

		tvdbAddress := row["tvdb"]
		episode := episodic.Episode{
			ProviderID: fmt.Sprintf("tvdb:%s:%d:%g", mapping.TVDBID, tvdbAddress.Season, tvdbAddress.Episode),
			ExternalIDs: []episodic.ExternalID{{
				Provider: "thexem", Namespace: "episode_mapping",
				Value: fmt.Sprintf("tvdb:%s:%d:%g", mapping.TVDBID, tvdbAddress.Season, tvdbAddress.Episode),
			}},
			Numbers: []episodic.EpisodeNumber{{
				Scheme: "aired", Season: canonical.Season, Number: canonical.Episode, Provider: "thexem",
			}},
			IsSpecial: canonical.Season == 0, EpisodeType: "regular",
		}
		if episode.IsSpecial {
			episode.EpisodeType = "special"
		}
		schemes := make([]string, 0, len(row))
		for scheme := range row {
			schemes = append(schemes, strings.ToLower(strings.TrimSpace(scheme)))
		}
		sort.Strings(schemes)
		for _, scheme := range schemes {
			address := row[scheme]
			if address.Episode <= 0 {
				continue
			}
			episode.Numbers = append(episode.Numbers, episodic.EpisodeNumber{
				Scheme: scheme, Season: address.Season, Number: address.Episode, Provider: "thexem",
			})
		}
		// The canonical AniDB/scene address can restart its absolute number for
		// each cour. TVDB's absolute number is series-wide and therefore the safe
		// cross-provider identity bridge when it is available.
		absolute := canonical.Absolute
		if tvdbAddress.Absolute > 0 {
			absolute = tvdbAddress.Absolute
		}
		if absolute > 0 {
			episode.Numbers = append(episode.Numbers, episodic.EpisodeNumber{
				Scheme: "absolute", Number: absolute, Provider: "thexem",
			})
		}
		record.Episodes = append(record.Episodes, episode)
	}
	sort.Slice(record.Seasons, func(i, j int) bool { return record.Seasons[i].Number < record.Seasons[j].Number })
	record.SeasonCount = len(record.Seasons)
	record.EpisodeCount = len(record.Episodes)
	return record, mapping, nil
}

// NormalizeTV retains every mapped numbering scheme without changing the
// canonical aired season chosen by the ordinary TV combiner. The same rows
// therefore help scanners translate scene/AniDB numbers for conventional TV,
// while only the anime normalizer uses XEM as a season-structure authority.
func NormalizeTV(payloads []providers.Payload, tvdbID, kind string) (episodic.NormalizedRecord, Mapping, error) {
	if len(payloads) == 0 {
		return episodic.NormalizedRecord{}, Mapping{}, fmt.Errorf("TheXEM returned no episode mapping")
	}
	mapping, err := ParseMapping(payloads[0], tvdbID)
	if err != nil {
		return episodic.NormalizedRecord{}, Mapping{}, err
	}
	record := episodic.NormalizedRecord{
		SchemaVersion: 1, Kind: kind, Provider: "thexem", Namespace: "mapping",
		ProviderID: "tvdb:" + mapping.TVDBID, PrimaryObservationID: payloads[0].ObservationID,
		ObservedAt: payloads[0].ObservedAt, NormalizerVersion: NormalizerVersion,
	}
	if len(payloads) > 1 && payloads[1].StatusCode == http.StatusOK {
		record.Titles = normalizeNames(payloads[1].Body)
	}
	for _, row := range mapping.Rows {
		tvdbAddress, ok := row["tvdb"]
		if !ok || tvdbAddress.Episode <= 0 {
			continue
		}
		episode := episodic.Episode{
			ProviderID: fmt.Sprintf("tvdb:%s:%d:%g", mapping.TVDBID, tvdbAddress.Season, tvdbAddress.Episode),
			ExternalIDs: []episodic.ExternalID{{
				Provider: "thexem", Namespace: "episode_mapping",
				Value: fmt.Sprintf("tvdb:%s:%d:%g", mapping.TVDBID, tvdbAddress.Season, tvdbAddress.Episode),
			}},
			EpisodeType: "regular", IsSpecial: tvdbAddress.Season == 0,
		}
		if episode.IsSpecial {
			episode.EpisodeType = "special"
		}
		schemes := make([]string, 0, len(row))
		for scheme := range row {
			schemes = append(schemes, strings.ToLower(strings.TrimSpace(scheme)))
		}
		sort.Strings(schemes)
		for _, scheme := range schemes {
			address := row[scheme]
			if address.Episode > 0 {
				episode.Numbers = append(episode.Numbers, episodic.EpisodeNumber{Scheme: scheme, Season: address.Season, Number: address.Episode, Provider: "thexem"})
			}
		}
		record.Episodes = append(record.Episodes, episode)
	}
	record.EpisodeCount = len(record.Episodes)
	return record, mapping, nil
}

func canonicalAnimeAddress(row Row) (Address, bool) {
	for _, scheme := range []string{"anidb", "scene", "tvdb"} {
		if address, ok := row[scheme]; ok && address.Episode > 0 {
			return address, true
		}
	}
	return Address{}, false
}

func normalizeNames(body []byte) []episodic.Title {
	var envelope struct {
		Result string                     `json:"result"`
		Data   map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || !strings.EqualFold(envelope.Result, "success") {
		return nil
	}
	result := []episodic.Title{}
	seen := map[string]bool{}
	appendName := func(value, language string) {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value) + "\x00" + strings.ToLower(language)
		if value == "" || seen[key] {
			return
		}
		seen[key] = true
		result = append(result, episodic.Title{Value: value, Language: language, Type: "alias"})
	}
	for _, raw := range envelope.Data {
		var value string
		if json.Unmarshal(raw, &value) == nil {
			appendName(value, "")
			continue
		}
		var languages map[string][]string
		if json.Unmarshal(raw, &languages) == nil {
			for language, values := range languages {
				for _, name := range values {
					appendName(name, language)
				}
			}
			continue
		}
	}
	return result
}
