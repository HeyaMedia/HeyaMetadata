package episodic

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/jackc/pgx/v5"
)

type ParentResource struct {
	EntityID string `json:"entity_id" format:"uuid"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	ImageID  string `json:"image_id,omitempty"`
}

type SeasonResource struct {
	ID       string         `json:"id" format:"uuid"`
	Show     ParentResource `json:"show"`
	Data     Season         `json:"data"`
	Episodes []Episode      `json:"episodes"`
}

type EpisodeResource struct {
	ID   string         `json:"id" format:"uuid"`
	Show ParentResource `json:"show"`
	Data Episode        `json:"data"`
}

func episodeIdentityKey(episode Episode) string {
	if len(episode.ExternalIDs) > 0 {
		ids := append([]ExternalID(nil), episode.ExternalIDs...)
		sort.Slice(ids, func(i, j int) bool {
			return ids[i].Provider+":"+ids[i].Namespace+":"+strings.ToLower(ids[i].Value) < ids[j].Provider+":"+ids[j].Namespace+":"+strings.ToLower(ids[j].Value)
		})
		id := ids[0]
		return "external:" + strings.ToLower(id.Provider) + ":" + strings.ToLower(id.Namespace) + ":" + strings.ToLower(strings.TrimSpace(id.Value))
	}
	if number, ok := preferredEpisodeNumber(episode.Numbers); ok {
		scheme := strings.ToLower(strings.TrimSpace(number.Scheme))
		if scheme == "" {
			scheme = "provider"
		}
		return fmt.Sprintf("%s:%d:%g", scheme, number.Season, number.Number)
	}
	return "provider:0:" + strings.TrimSpace(episode.ProviderID)
}

func persistResources(ctx context.Context, tx pgx.Tx, showID, kind string, record *NormalizedRecord) error {
	seasonIDs := make(map[int]string, len(record.Seasons))
	allocatedSeasonIDs := make(map[string]int, len(record.Seasons))
	for i := range record.Seasons {
		season := &record.Seasons[i]
		if len(season.Titles) == 0 && season.Name != "" {
			season.Titles = []Title{{Value: season.Name, Type: "display"}}
		}
		if season.Name == "" && len(season.Titles) > 0 {
			season.Name = season.Titles[0].Value
		}
		var id string
		for _, external := range season.ExternalIDs {
			err := tx.QueryRow(ctx, `SELECT season_id::text FROM episodic_season_external_ids WHERE show_entity_id=$1 AND provider=$2 AND namespace=$3 AND normalized_value=$4`, showID, strings.ToLower(external.Provider), strings.ToLower(external.Namespace), strings.ToLower(strings.TrimSpace(external.Value))).Scan(&id)
			if err != nil && err != pgx.ErrNoRows {
				return err
			}
			if allocatedNumber, allocated := allocatedSeasonIDs[id]; allocated && allocatedNumber != season.Number {
				id = ""
				continue
			}
			if id != "" {
				break
			}
		}
		if id == "" {
			err := tx.QueryRow(ctx, `SELECT id::text FROM episodic_seasons WHERE show_entity_id=$1 AND season_number=$2`, showID, season.Number).Scan(&id)
			if err != nil && err != pgx.ErrNoRows {
				return err
			}
		}
		if id == "" {
			if err := tx.QueryRow(ctx, `INSERT INTO episodic_seasons(show_entity_id,show_kind,season_number,document)VALUES($1,$2,$3,'{}')RETURNING id`, showID, kind, season.Number).Scan(&id); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE episodic_seasons SET show_kind=$2,season_number=$3,updated_at=now() WHERE id=$1`, id, kind, season.Number); err != nil {
			return err
		}
		season.ID = id
		seasonIDs[season.Number] = id
		allocatedSeasonIDs[id] = season.Number
		for _, external := range season.ExternalIDs {
			if external.Value == "" {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO episodic_season_external_ids(season_id,show_entity_id,provider,namespace,normalized_value)VALUES($1,$2,$3,$4,$5)ON CONFLICT(show_entity_id,provider,namespace,normalized_value)DO UPDATE SET season_id=EXCLUDED.season_id`, id, showID, strings.ToLower(external.Provider), strings.ToLower(external.Namespace), strings.ToLower(strings.TrimSpace(external.Value))); err != nil {
				return err
			}
		}
	}
	// Reserve rows whose stable identity keys still appear in this projection
	// before consulting looser external-ID history. This lets a refresh split a
	// previously merged episode without whichever half sorts first stealing the
	// legacy UUID from the half it originally represented.
	existingEpisodeIDsByIdentity := make(map[string]string, len(record.Episodes))
	reservedEpisodeIdentities := make(map[string]string, len(record.Episodes))
	for i := range record.Episodes {
		normalizeEpisode(&record.Episodes[i])
		identityKey := episodeIdentityKey(record.Episodes[i])
		var id string
		err := tx.QueryRow(ctx, `SELECT id::text FROM episodic_episodes WHERE show_entity_id=$1 AND identity_key=$2`, showID, identityKey).Scan(&id)
		if err != nil && err != pgx.ErrNoRows {
			return fmt.Errorf("find episode identity %q: %w", identityKey, err)
		}
		if id != "" {
			existingEpisodeIDsByIdentity[identityKey] = id
			reservedEpisodeIdentities[id] = identityKey
		}
	}
	allocatedEpisodeIDs := make(map[string]struct{}, len(record.Episodes))
	for i := range record.Episodes {
		episode := &record.Episodes[i]
		var seasonID any
		if id := seasonIDs[episodeSeasonNumber(*episode)]; id != "" {
			seasonID = id
		}
		identityKey := episodeIdentityKey(*episode)
		id := existingEpisodeIDsByIdentity[identityKey]
		if _, allocated := allocatedEpisodeIDs[id]; allocated {
			id = ""
		}
		for _, external := range episode.ExternalIDs {
			if id != "" {
				break
			}
			var candidateID string
			err := tx.QueryRow(ctx, `SELECT episode_id::text FROM episodic_episode_external_ids WHERE show_entity_id=$1 AND provider=$2 AND namespace=$3 AND normalized_value=$4`, showID, strings.ToLower(external.Provider), strings.ToLower(external.Namespace), strings.ToLower(strings.TrimSpace(external.Value))).Scan(&candidateID)
			if err != nil && err != pgx.ErrNoRows {
				return err
			}
			if candidateID == "" {
				continue
			}
			if _, allocated := allocatedEpisodeIDs[candidateID]; allocated {
				continue
			}
			if reservedIdentity, reserved := reservedEpisodeIdentities[candidateID]; reserved && reservedIdentity != identityKey {
				continue
			}
			id = candidateID
		}
		if id == "" {
			if err := tx.QueryRow(ctx, `INSERT INTO episodic_episodes(show_entity_id,show_kind,season_id,identity_key,document)VALUES($1,$2,$3,$4,'{}')RETURNING id`, showID, kind, seasonID, identityKey).Scan(&id); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE episodic_episodes SET show_kind=$2,season_id=$3,updated_at=now() WHERE id=$1`, id, kind, seasonID); err != nil {
			return err
		}
		episode.ID = id
		allocatedEpisodeIDs[id] = struct{}{}
		if value, ok := seasonID.(string); ok {
			episode.SeasonID = value
		}
		for _, external := range episode.ExternalIDs {
			if external.Value == "" {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO episodic_episode_external_ids(episode_id,show_entity_id,provider,namespace,normalized_value)VALUES($1,$2,$3,$4,$5)ON CONFLICT(show_entity_id,provider,namespace,normalized_value)DO UPDATE SET episode_id=EXCLUDED.episode_id`, id, showID, strings.ToLower(external.Provider), strings.ToLower(external.Namespace), strings.ToLower(strings.TrimSpace(external.Value))); err != nil {
				return err
			}
		}
	}
	sortEpisodes(record.Episodes)
	seasonByID := map[string]*Season{}
	reportedEpisodeCounts := map[string]int{}
	for i := range record.Seasons {
		reportedEpisodeCounts[record.Seasons[i].ID] = max(record.Seasons[i].EpisodeCount, record.Seasons[i].EpisodeOrder)
		record.Seasons[i].EpisodeIDs = []string{}
		record.Seasons[i].EpisodeCount = 0
		record.Seasons[i].AiredEpisodeCount = 0
		seasonByID[record.Seasons[i].ID] = &record.Seasons[i]
	}
	for i := range record.Episodes {
		episode := &record.Episodes[i]
		if season := seasonByID[episode.SeasonID]; season != nil {
			season.EpisodeIDs = append(season.EpisodeIDs, episode.ID)
			season.EpisodeCount++
			if episode.AirDate != "" && episode.AirDate <= time.Now().UTC().Format("2006-01-02") {
				season.AiredEpisodeCount++
			}
		}
		body, _ := json.Marshal(episode)
		if _, err := tx.Exec(ctx, `UPDATE episodic_episodes SET document=$2,updated_at=now() WHERE id=$1`, episode.ID, body); err != nil {
			return err
		}
	}
	for i := range record.Seasons {
		season := &record.Seasons[i]
		season.EpisodeCount = max(season.EpisodeCount, reportedEpisodeCounts[season.ID])
		body, _ := json.Marshal(season)
		if _, err := tx.Exec(ctx, `UPDATE episodic_seasons SET document=$2,updated_at=now() WHERE id=$1`, season.ID, body); err != nil {
			return err
		}
	}
	seasonResourceIDs := make([]string, 0, len(record.Seasons))
	for _, season := range record.Seasons {
		seasonResourceIDs = append(seasonResourceIDs, season.ID)
	}
	episodeResourceIDs := make([]string, 0, len(record.Episodes))
	for _, episode := range record.Episodes {
		episodeResourceIDs = append(episodeResourceIDs, episode.ID)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM image_candidates WHERE entity_id=$1 AND ownership_scope='episode' AND owner_resource_id IS NOT NULL AND NOT(owner_resource_id=ANY($2::uuid[]))`, showID, episodeResourceIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM image_candidates WHERE entity_id=$1 AND ownership_scope='season' AND owner_resource_id IS NOT NULL AND NOT(owner_resource_id=ANY($2::uuid[]))`, showID, seasonResourceIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM episodic_episodes WHERE show_entity_id=$1 AND NOT(id=ANY($2::uuid[]))`, showID, episodeResourceIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM episodic_seasons WHERE show_entity_id=$1 AND NOT(id=ANY($2::uuid[]))`, showID, seasonResourceIDs); err != nil {
		return err
	}
	return nil
}

func hydrateResources(ctx context.Context, runtime *platform.Runtime, showID string, doc *Document) error {
	for index := range doc.Data.Networks {
		network := &doc.Data.Networks[index]
		network.ResolutionState = "unresolved"
		if network.EntityID != "" {
			network.ResolutionState = "materialized"
		}
	}
	for index := range doc.Data.Organizations {
		organization := &doc.Data.Organizations[index]
		organization.ResolutionState = "unresolved"
		if organization.EntityID != "" {
			organization.ResolutionState = "materialized"
		}
	}
	for index := range doc.Data.Recommendations {
		recommendation := &doc.Data.Recommendations[index]
		if recommendation.EntityID == "" {
			for _, external := range recommendation.ExternalIDs {
				_ = runtime.DB.QueryRow(ctx, `SELECT claim.entity_id::text FROM external_id_claims claim JOIN entities entity ON entity.id=claim.entity_id AND entity.kind=$1 AND entity.deleted_at IS NULL WHERE claim.entity_kind=$1 AND claim.provider=$2 AND claim.namespace=$3 AND claim.normalized_value=$4 AND claim.state='accepted' LIMIT 1`, doc.Kind, external.Provider, external.Namespace, strings.ToLower(external.Value)).Scan(&recommendation.EntityID)
				if recommendation.EntityID != "" {
					break
				}
			}
		}
		recommendation.ResolutionState = "unresolved"
		if recommendation.EntityID != "" {
			recommendation.ResolutionState = "materialized"
		}
	}
	if len(doc.Data.Credits) > 0 {
		providers := make([]string, 0, len(doc.Data.Credits))
		values := make([]string, 0, len(doc.Data.Credits))
		for _, credit := range doc.Data.Credits {
			providers = append(providers, credit.Provider)
			values = append(values, credit.ProviderPersonID)
		}
		rows, err := runtime.DB.Query(ctx, `SELECT provider,normalized_value,entity_id::text FROM external_id_claims WHERE entity_kind='person' AND namespace='person' AND state='accepted' AND provider=ANY($1) AND normalized_value=ANY($2)`, providers, values)
		if err != nil {
			return err
		}
		resolved := map[string]string{}
		for rows.Next() {
			var provider, value, entityID string
			if err := rows.Scan(&provider, &value, &entityID); err != nil {
				rows.Close()
				return err
			}
			resolved[provider+":"+value] = entityID
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for i := range doc.Data.Credits {
			credit := &doc.Data.Credits[i]
			credit.PersonEntityID = resolved[credit.Provider+":"+credit.ProviderPersonID]
		}
	}
	seasonRows, err := runtime.DB.Query(ctx, `SELECT id,document FROM episodic_seasons WHERE show_entity_id=$1 ORDER BY season_number`, showID)
	if err != nil {
		return err
	}
	defer seasonRows.Close()
	var seasons []Season
	for seasonRows.Next() {
		var id string
		var body []byte
		if err := seasonRows.Scan(&id, &body); err != nil {
			return err
		}
		var season Season
		if err := json.Unmarshal(body, &season); err != nil {
			return err
		}
		season.ID = id
		seasons = append(seasons, season)
	}
	if err := seasonRows.Err(); err != nil {
		return err
	}
	episodeRows, err := runtime.DB.Query(ctx, `SELECT id,season_id,document FROM episodic_episodes WHERE show_entity_id=$1`, showID)
	if err != nil {
		return err
	}
	defer episodeRows.Close()
	var episodes []Episode
	for episodeRows.Next() {
		var id string
		var seasonID *string
		var body []byte
		if err := episodeRows.Scan(&id, &seasonID, &body); err != nil {
			return err
		}
		var episode Episode
		if err := json.Unmarshal(body, &episode); err != nil {
			return err
		}
		episode.ID = id
		if seasonID != nil {
			episode.SeasonID = *seasonID
		}
		episodes = append(episodes, episode)
	}
	if err := episodeRows.Err(); err != nil {
		return err
	}
	if seasons != nil {
		doc.Data.Seasons = seasons
	}
	if episodes != nil {
		sortEpisodes(episodes)
		doc.Data.Episodes = episodes
	}
	return nil
}

func SeasonDetail(ctx context.Context, runtime *platform.Runtime, id string) (SeasonResource, error) {
	var result SeasonResource
	var body []byte
	if err := runtime.DB.QueryRow(ctx, `SELECT s.id,s.show_entity_id,s.show_kind,se.display_title,COALESCE(se.summary->'display'->>'image_id',''),s.document FROM episodic_seasons s JOIN entities e ON e.id=s.show_entity_id AND e.deleted_at IS NULL JOIN search_entities se ON se.entity_id=s.show_entity_id WHERE s.id=$1`, id).Scan(&result.ID, &result.Show.EntityID, &result.Show.Kind, &result.Show.Title, &result.Show.ImageID, &body); err != nil {
		return result, err
	}
	if err := json.Unmarshal(body, &result.Data); err != nil {
		return result, err
	}
	result.Data.ID = result.ID
	rows, err := runtime.DB.Query(ctx, `SELECT id,season_id,document FROM episodic_episodes WHERE season_id=$1`, id)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var episode Episode
		var episodeID string
		var seasonID *string
		var episodeBody []byte
		if err := rows.Scan(&episodeID, &seasonID, &episodeBody); err != nil {
			return result, err
		}
		if err := json.Unmarshal(episodeBody, &episode); err != nil {
			return result, err
		}
		episode.ID = episodeID
		if seasonID != nil {
			episode.SeasonID = *seasonID
		}
		result.Episodes = append(result.Episodes, episode)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if result.Episodes == nil {
		result.Episodes = []Episode{}
	}
	sortEpisodes(result.Episodes)
	return result, nil
}

func preferredEpisodeNumber(numbers []EpisodeNumber) (EpisodeNumber, bool) {
	priority := map[string]int{"aired": 0, "dvd": 1, "tmdb": 2, "tvdb": 3, "tvmaze": 4, "anidb": 5, "absolute": 6, "special": 7, "credit": 8, "trailer": 9, "parody": 10}
	if len(numbers) == 0 {
		return EpisodeNumber{}, false
	}
	values := append([]EpisodeNumber(nil), numbers...)
	sort.SliceStable(values, func(i, j int) bool {
		left, right := priority[strings.ToLower(values[i].Scheme)], priority[strings.ToLower(values[j].Scheme)]
		if left == 0 && strings.ToLower(values[i].Scheme) != "aired" {
			left = 100
		}
		if right == 0 && strings.ToLower(values[j].Scheme) != "aired" {
			right = 100
		}
		if left != right {
			return left < right
		}
		leftProvider, rightProvider := episodeNumberProviderPriority(values[i].Provider), episodeNumberProviderPriority(values[j].Provider)
		if leftProvider != rightProvider {
			return leftProvider < rightProvider
		}
		if values[i].Season != values[j].Season {
			return values[i].Season < values[j].Season
		}
		return values[i].Number < values[j].Number
	})
	return values[0], true
}

func episodeNumberProviderPriority(provider string) int {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "thexem":
		return -1
	case "tvmaze":
		return 0
	case "anidb":
		return 1
	case "tmdb":
		return 2
	case "tvdb":
		return 3
	default:
		return 100
	}
}

func episodeSeasonNumber(episode Episode) int {
	if number, ok := preferredEpisodeNumber(episode.Numbers); ok {
		if number.Scheme == "absolute" && number.Season == 0 && !episode.IsSpecial {
			return 1
		}
		return number.Season
	}
	if episode.IsSpecial {
		return 0
	}
	return 1
}

func sortEpisodes(episodes []Episode) {
	sort.SliceStable(episodes, func(i, j int) bool {
		left, leftOK := preferredEpisodeNumber(episodes[i].Numbers)
		right, rightOK := preferredEpisodeNumber(episodes[j].Numbers)
		if leftOK != rightOK {
			return leftOK
		}
		if left.Season != right.Season {
			return left.Season < right.Season
		}
		if episodes[i].IsSpecial != episodes[j].IsSpecial {
			return !episodes[i].IsSpecial
		}
		if left.Number != right.Number {
			return left.Number < right.Number
		}
		if episodes[i].AirDate != episodes[j].AirDate {
			return episodes[i].AirDate < episodes[j].AirDate
		}
		return episodes[i].ID < episodes[j].ID
	})
}

func EpisodeDetail(ctx context.Context, runtime *platform.Runtime, id string) (EpisodeResource, error) {
	var result EpisodeResource
	var body []byte
	var seasonID *string
	if err := runtime.DB.QueryRow(ctx, `SELECT ep.id,ep.show_entity_id,ep.show_kind,se.display_title,COALESCE(se.summary->'display'->>'image_id',''),ep.season_id,ep.document FROM episodic_episodes ep JOIN entities e ON e.id=ep.show_entity_id AND e.deleted_at IS NULL JOIN search_entities se ON se.entity_id=ep.show_entity_id WHERE ep.id=$1`, id).Scan(&result.ID, &result.Show.EntityID, &result.Show.Kind, &result.Show.Title, &result.Show.ImageID, &seasonID, &body); err != nil {
		return result, err
	}
	if err := json.Unmarshal(body, &result.Data); err != nil {
		return result, err
	}
	result.Data.ID = result.ID
	if seasonID != nil {
		result.Data.SeasonID = *seasonID
	}
	return result, nil
}
