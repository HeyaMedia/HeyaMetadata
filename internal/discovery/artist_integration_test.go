package discovery

import (
	"context"
	"os"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/google/uuid"
)

func TestIntegrationPersistedArtistReleaseBridgeRequiresCanonicalClaimsAndRelationship(t *testing.T) {
	if os.Getenv("HEYA_METADATA_INTEGRATION") != "1" {
		t.Skip("set HEYA_METADATA_INTEGRATION=1 to use the local platform stack")
	}
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := platform.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)

	artistSlug := "artist-release-bridge-" + uuid.NewString()
	releaseSlug := "artist-release-group-bridge-" + uuid.NewString()
	var artistID, releaseGroupID string
	if err := runtime.DB.QueryRow(ctx, `INSERT INTO entities(kind,slug)VALUES('artist',$1)RETURNING id::text`, artistSlug).Scan(&artistID); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `INSERT INTO entities(kind,slug)VALUES('release_group',$1)RETURNING id::text`, releaseSlug).Scan(&releaseGroupID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM entity_relations WHERE source_entity_id=$1 OR target_entity_id=$2`, artistID, releaseGroupID)
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM canonical_release_groups WHERE entity_id=$1`, releaseGroupID)
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM external_id_claims WHERE entity_id=ANY($1::uuid[])`, []string{artistID, releaseGroupID})
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM entities WHERE id=ANY($1::uuid[])`, []string{artistID, releaseGroupID})
	})

	deezerArtistID := "bridge-artist-" + uuid.NewString()
	deezerAlbumID := "bridge-album-" + uuid.NewString()
	mbReleaseGroupID := uuid.NewString()
	mbArtistID := uuid.NewString()
	for _, claim := range []struct {
		entityID, kind, provider, namespace, value string
	}{
		{artistID, "artist", "deezer", "artist", deezerArtistID},
		{releaseGroupID, "release_group", "deezer", "album", deezerAlbumID},
		{releaseGroupID, "release_group", "musicbrainz", "release_group", mbReleaseGroupID},
	} {
		if _, err := runtime.DB.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,confidence,first_observed_at,last_observed_at)VALUES($1,$2,$3,$4,$5,'accepted',1,now(),now())`, claim.entityID, claim.kind, claim.provider, claim.namespace, claim.value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runtime.DB.Exec(ctx, `INSERT INTO entity_relations(source_entity_id,target_entity_id,source_kind,target_kind,relation_type,provider,namespace,provider_value,state)VALUES($1,$2,'artist','release_group','discography','deezer','album',$3,'accepted')`, artistID, releaseGroupID, deezerAlbumID); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.Exec(ctx, `INSERT INTO canonical_release_groups(entity_id,merge_version,source_fingerprint,document)VALUES($1,'integration','integration',jsonb_build_object('data',jsonb_build_object('artist_credits',jsonb_build_array(jsonb_build_object('artist_provider','musicbrainz','artist_namespace','artist','artist_id',$2::text)))))`, releaseGroupID, mbArtistID); err != nil {
		t.Fatal(err)
	}

	bridge := artistReleaseBridge{
		EntityID: artistID,
		MusicBrainz: artistReleaseMatch{
			Artist:  ExternalID{Provider: "musicbrainz", Namespace: "artist", Value: mbArtistID},
			Release: ExternalID{Provider: "musicbrainz", Namespace: "release_group", Value: mbReleaseGroupID},
		},
		Storefront: artistReleaseMatch{
			Artist:  ExternalID{Provider: "deezer", Namespace: "artist", Value: deezerArtistID},
			Release: ExternalID{Provider: "deezer", Namespace: "album", Value: deezerAlbumID},
		},
	}
	matched, err := NewService(runtime).persistedArtistReleaseBridge(ctx, bridge)
	if err != nil || !matched {
		t.Fatalf("persisted bridge matched=%v err=%v", matched, err)
	}
	if _, err := runtime.DB.Exec(ctx, `UPDATE canonical_release_groups SET document=jsonb_set(document,'{data,artist_credits,0,artist_id}',to_jsonb($2::text)) WHERE entity_id=$1`, releaseGroupID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	matched, err = NewService(runtime).persistedArtistReleaseBridge(ctx, bridge)
	if err != nil || matched {
		t.Fatalf("bridge without persisted MusicBrainz credit matched=%v err=%v", matched, err)
	}
	if _, err := runtime.DB.Exec(ctx, `UPDATE canonical_release_groups SET document=jsonb_set(document,'{data,artist_credits,0,artist_id}',to_jsonb($2::text)) WHERE entity_id=$1`, releaseGroupID, mbArtistID); err != nil {
		t.Fatal(err)
	}

	missingClaim := bridge
	missingClaim.Storefront.Release.Value = "different-album"
	matched, err = NewService(runtime).persistedArtistReleaseBridge(ctx, missingClaim)
	if err != nil || matched {
		t.Fatalf("bridge without shared release claim matched=%v err=%v", matched, err)
	}
	if _, err := runtime.DB.Exec(ctx, `UPDATE entity_relations SET state='superseded' WHERE source_entity_id=$1 AND target_entity_id=$2`, artistID, releaseGroupID); err != nil {
		t.Fatal(err)
	}
	matched, err = NewService(runtime).persistedArtistReleaseBridge(ctx, bridge)
	if err != nil || matched {
		t.Fatalf("bridge without accepted artist relationship matched=%v err=%v", matched, err)
	}
}
