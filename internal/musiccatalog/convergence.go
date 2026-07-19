package musiccatalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
)

// ArtistIdentityBridge is the minimal persisted proof that one unclaimed
// MusicBrainz artist root belongs on an existing storefront-backed canonical
// artist. Names, titles, dates, and unclaimed provider roots are deliberately
// absent: both provider releases must already resolve to the same canonical
// release group related to the accepted storefront artist root.
type ArtistIdentityBridge struct {
	ArtistEntityID             string
	MusicBrainzArtistID        string
	MusicBrainzReleaseGroupID  string
	StorefrontProvider         string
	StorefrontArtistID         string
	StorefrontReleaseNamespace string
	StorefrontReleaseID        string
}

func PersistedArtistIdentityBridge(ctx context.Context, runtime *platform.Runtime, bridge ArtistIdentityBridge) (bool, error) {
	bridge.ArtistEntityID = strings.TrimSpace(bridge.ArtistEntityID)
	bridge.MusicBrainzArtistID = strings.ToLower(strings.TrimSpace(bridge.MusicBrainzArtistID))
	bridge.MusicBrainzReleaseGroupID = strings.ToLower(strings.TrimSpace(bridge.MusicBrainzReleaseGroupID))
	bridge.StorefrontProvider = strings.ToLower(strings.TrimSpace(bridge.StorefrontProvider))
	bridge.StorefrontArtistID = strings.ToLower(strings.TrimSpace(bridge.StorefrontArtistID))
	bridge.StorefrontReleaseNamespace = strings.ToLower(strings.TrimSpace(bridge.StorefrontReleaseNamespace))
	bridge.StorefrontReleaseID = strings.ToLower(strings.TrimSpace(bridge.StorefrontReleaseID))
	if bridge.ArtistEntityID == "" || bridge.MusicBrainzArtistID == "" || bridge.MusicBrainzReleaseGroupID == "" ||
		(bridge.StorefrontProvider != "apple" && bridge.StorefrontProvider != "deezer") || bridge.StorefrontArtistID == "" ||
		bridge.StorefrontReleaseNamespace == "" || bridge.StorefrontReleaseID == "" {
		return false, nil
	}
	var matched bool
	err := runtime.DB.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM external_id_claims artist_claim
			JOIN entities artist_entity ON artist_entity.id=artist_claim.entity_id
			  AND artist_entity.kind='artist' AND artist_entity.deleted_at IS NULL
			JOIN entity_relations relation ON relation.source_entity_id=artist_claim.entity_id
			  AND relation.source_kind='artist' AND relation.target_kind='release_group'
			  AND relation.relation_type='discography' AND relation.state='accepted'
			JOIN entities release_group ON release_group.id=relation.target_entity_id
			  AND release_group.kind='release_group' AND release_group.deleted_at IS NULL
			JOIN canonical_release_groups canonical ON canonical.entity_id=release_group.id
			JOIN external_id_claims mb_release ON mb_release.entity_id=release_group.id
			  AND mb_release.entity_kind='release_group' AND mb_release.provider='musicbrainz'
			  AND mb_release.namespace='release_group' AND mb_release.normalized_value=$5
			  AND mb_release.state='accepted'
			JOIN external_id_claims storefront_release ON storefront_release.entity_id=release_group.id
			  AND storefront_release.entity_kind='release_group' AND storefront_release.provider=$2
			  AND storefront_release.namespace=$6 AND storefront_release.normalized_value=$7
			  AND storefront_release.state='accepted'
			WHERE artist_claim.entity_id=$1 AND artist_claim.entity_kind='artist'
			  AND artist_claim.provider=$2 AND artist_claim.namespace='artist'
			  AND artist_claim.normalized_value=$3 AND artist_claim.state='accepted'
			  AND EXISTS(
				SELECT 1
				FROM jsonb_array_elements(COALESCE(canonical.document#>'{data,artist_credits}','[]'::jsonb)) credit
				WHERE lower(COALESCE(credit->>'artist_provider',''))='musicbrainz'
				  AND lower(COALESCE(credit->>'artist_namespace',''))='artist'
				  AND lower(COALESCE(credit->>'artist_id',''))=$4
			  )
		)`, bridge.ArtistEntityID,
		bridge.StorefrontProvider,
		bridge.StorefrontArtistID,
		bridge.MusicBrainzArtistID,
		bridge.MusicBrainzReleaseGroupID,
		bridge.StorefrontReleaseNamespace,
		bridge.StorefrontReleaseID,
	).Scan(&matched)
	if err != nil {
		return false, fmt.Errorf("check persisted artist identity bridge: %w", err)
	}
	return matched, nil
}
