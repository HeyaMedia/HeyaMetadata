package episodic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/jackc/pgx/v5"
)

type ParentResource struct {
	EntityID string `json:"entity_id"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	ImageID  string `json:"image_id,omitempty"`
}

type SeasonResource struct {
	ID       string         `json:"id"`
	Show     ParentResource `json:"show"`
	Data     Season         `json:"data"`
	Episodes []Episode      `json:"episodes"`
}

type EpisodeResource struct {
	ID   string         `json:"id"`
	Show ParentResource `json:"show"`
	Data Episode        `json:"data"`
}

func episodeIdentityKey(episode Episode) string {
	if len(episode.Numbers) > 0 {
		number := episode.Numbers[0]
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
	for i := range record.Seasons {
		season := &record.Seasons[i]
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO episodic_seasons(show_entity_id,show_kind,season_number,document)VALUES($1,$2,$3,'{}')ON CONFLICT(show_entity_id,season_number)DO UPDATE SET show_kind=EXCLUDED.show_kind,updated_at=now() RETURNING id`, showID, kind, season.Number).Scan(&id); err != nil {
			return err
		}
		season.ID = id
		seasonIDs[season.Number] = id
		body, _ := json.Marshal(season)
		if _, err := tx.Exec(ctx, `UPDATE episodic_seasons SET document=$2,updated_at=now() WHERE id=$1`, id, body); err != nil {
			return err
		}
	}
	for i := range record.Episodes {
		episode := &record.Episodes[i]
		var seasonID any
		if len(episode.Numbers) > 0 {
			if id := seasonIDs[episode.Numbers[0].Season]; id != "" {
				seasonID = id
			}
		}
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO episodic_episodes(show_entity_id,show_kind,season_id,identity_key,document)VALUES($1,$2,$3,$4,'{}')ON CONFLICT(show_entity_id,identity_key)DO UPDATE SET show_kind=EXCLUDED.show_kind,season_id=EXCLUDED.season_id,updated_at=now() RETURNING id`, showID, kind, seasonID, episodeIdentityKey(*episode)).Scan(&id); err != nil {
			return err
		}
		episode.ID = id
		if value, ok := seasonID.(string); ok {
			episode.SeasonID = value
		}
		body, _ := json.Marshal(episode)
		if _, err := tx.Exec(ctx, `UPDATE episodic_episodes SET document=$2,updated_at=now() WHERE id=$1`, id, body); err != nil {
			return err
		}
	}
	return nil
}

func hydrateResources(ctx context.Context, runtime *platform.Runtime, showID string, doc *Document) error {
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
	episodeRows, err := runtime.DB.Query(ctx, `SELECT id,season_id,document FROM episodic_episodes WHERE show_entity_id=$1 ORDER BY identity_key`, showID)
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
	rows, err := runtime.DB.Query(ctx, `SELECT id,season_id,document FROM episodic_episodes WHERE season_id=$1 ORDER BY identity_key`, id)
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
	return result, rows.Err()
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
