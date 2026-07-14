// Package images safely materializes provider image candidates into permanent storage.
package images

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/blobstore"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/jackc/pgx/v5"
)

const MaxOriginalBytes int64 = 25 * 1024 * 1024

var (
	ErrNotFound   = errors.New("image not found")
	ErrNotReady   = errors.New("image is not materialized")
	ErrInProgress = errors.New("image materialization is already in progress")
)

type Service struct {
	runtime   *platform.Runtime
	client    *http.Client
	allowHTTP bool
}
type Asset struct {
	ID, ObjectKey, MediaType, Checksum string
	ByteSize                           int64
	Width, Height                      int
}

func NewService(runtime *platform.Runtime) *Service {
	return &Service{runtime: runtime, client: safeHTTPClient()}
}

func safeHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ForceAttemptHTTP2: true, MaxIdleConns: 20, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second}}
}

func (s *Service) Materialize(ctx context.Context, id string) (asset Asset, returnErr error) {
	var provider, sourceURL, class, state string
	err := s.runtime.DB.QueryRow(ctx, `
		UPDATE image_candidates candidate
		SET materialization_state='working',materialization_error=NULL,materialization_attempted_at=now()
		WHERE candidate.id=$1
		  AND (
			candidate.materialization_state IN ('pending','failed')
			OR (
				candidate.materialization_state='ready'
				AND NOT EXISTS (
					SELECT 1 FROM image_variants variant
					WHERE variant.image_id=candidate.id AND variant.transform_version=$2
				)
			)
		  )
		RETURNING provider,source_url,class,materialization_state`, id, TransformVersion).Scan(&provider, &sourceURL, &class, &state)
	if err == pgx.ErrNoRows {
		var existing Asset
		var existingState string
		readErr := s.runtime.DB.QueryRow(ctx, `SELECT id,COALESCE(object_key,''),COALESCE(media_type,''),COALESCE(blob_checksum,''),COALESCE(byte_size,0),COALESCE(materialized_width,0),COALESCE(materialized_height,0),materialization_state FROM image_candidates WHERE id=$1`, id).Scan(&existing.ID, &existing.ObjectKey, &existing.MediaType, &existing.Checksum, &existing.ByteSize, &existing.Width, &existing.Height, &existingState)
		if readErr == pgx.ErrNoRows {
			return Asset{}, ErrNotFound
		}
		if readErr != nil {
			return Asset{}, readErr
		}
		if existingState == "ready" {
			return existing, nil
		}
		return Asset{}, ErrInProgress
	}
	if err != nil {
		return Asset{}, fmt.Errorf("claim image candidate: %w", err)
	}
	defer func() {
		if returnErr != nil {
			_, _ = s.runtime.DB.Exec(context.WithoutCancel(ctx), `UPDATE image_candidates SET materialization_state='failed',materialization_error=$2 WHERE id=$1`, id, returnErr.Error())
		}
	}()
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return Asset{}, fmt.Errorf("parse image source: %w", err)
	}
	if err := validateSourceURL(parsed, s.allowHTTP); err != nil {
		return Asset{}, err
	}
	if !providerHostAllowed(provider, parsed.Hostname()) {
		return Asset{}, fmt.Errorf("image host %q is not allowed for provider %s", parsed.Hostname(), provider)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return Asset{}, err
	}
	request.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/gif;q=0.8")
	request.Header.Set("User-Agent", s.runtime.Config.Providers.MusicBrainz.UserAgent)
	client := *s.client
	client.CheckRedirect = func(request *http.Request, _ []*http.Request) error {
		if err := validateSourceURL(request.URL, s.allowHTTP); err != nil {
			return err
		}
		if !providerHostAllowed(provider, request.URL.Hostname()) {
			return fmt.Errorf("redirected image host %q is not allowed for provider %s", request.URL.Hostname(), provider)
		}
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		return Asset{}, fmt.Errorf("fetch image: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Asset{}, fmt.Errorf("image source returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxOriginalBytes+1))
	if err != nil {
		return Asset{}, fmt.Errorf("read image: %w", err)
	}
	if int64(len(body)) > MaxOriginalBytes {
		return Asset{}, fmt.Errorf("image exceeds %d byte limit", MaxOriginalBytes)
	}
	mediaType := normalizedImageType(http.DetectContentType(body))
	if mediaType == "" {
		return Asset{}, fmt.Errorf("source is not a supported image")
	}
	if declared := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0])); declared != "" && !strings.HasPrefix(declared, "image/") {
		return Asset{}, fmt.Errorf("source declared non-image content type %s", declared)
	}
	digest := sha256.Sum256(body)
	checksum := hex.EncodeToString(digest[:])
	objectKey, err := s.runtime.Blobs.ContentKeyUnder("images/original", checksum, imageSuffix(mediaType))
	if err != nil {
		return Asset{}, err
	}
	if err := registerObject(ctx, s.runtime, objectKey, mediaType, int64(len(body))); err != nil {
		return Asset{}, err
	}
	if err := s.runtime.Blobs.PutImmutable(ctx, objectKey, body, mediaType, ""); err != nil {
		return Asset{}, err
	}
	sourceWidth, sourceHeight, variants, err := buildVariants(body, class)
	if err != nil {
		return Asset{}, err
	}
	for index := range variants {
		variant := &variants[index]
		variant.ObjectKey, err = s.runtime.Blobs.ContentKeyUnder("images/derived/"+TransformVersion+"/"+variant.Format, variant.Checksum, "."+variant.Format)
		if err != nil {
			return Asset{}, err
		}
		if err := registerObject(ctx, s.runtime, variant.ObjectKey, variant.MediaType, variant.ByteSize); err != nil {
			return Asset{}, err
		}
		if err := s.runtime.Blobs.PutImmutable(ctx, variant.ObjectKey, variant.Body, variant.MediaType, ""); err != nil {
			return Asset{}, err
		}
	}
	asset = Asset{ID: id, ObjectKey: objectKey, MediaType: mediaType, Checksum: checksum, ByteSize: int64(len(body)), Width: sourceWidth, Height: sourceHeight}
	tx, err := s.runtime.DB.Begin(ctx)
	if err != nil {
		return Asset{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM image_variants WHERE image_id=$1`, id); err != nil {
		return Asset{}, fmt.Errorf("replace image variants: %w", err)
	}
	for _, variant := range variants {
		if _, err := tx.Exec(ctx, `INSERT INTO image_variants(image_id,transform_version,format,width,height,checksum,object_key,media_type,byte_size) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, TransformVersion, variant.Format, variant.Width, variant.Height, variant.Checksum, variant.ObjectKey, variant.MediaType, variant.ByteSize); err != nil {
			return Asset{}, fmt.Errorf("record image variant: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE image_candidates SET materialization_state='ready',blob_checksum=$2,object_key=$3,media_type=$4,byte_size=$5,materialization_error=NULL,materialized_at=now(),last_accessed_at=now(),materialized_width=$6,materialized_height=$7,evicted_at=NULL WHERE id=$1`, id, checksum, objectKey, mediaType, len(body), sourceWidth, sourceHeight); err != nil {
		return Asset{}, fmt.Errorf("finish image materialization: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Asset{}, fmt.Errorf("commit image materialization: %w", err)
	}
	return asset, nil
}

func (s *Service) Read(ctx context.Context, id string) (Asset, []byte, error) {
	var asset Asset
	var state string
	err := s.runtime.DB.QueryRow(ctx, `SELECT id,COALESCE(object_key,''),COALESCE(media_type,''),COALESCE(blob_checksum,''),COALESCE(byte_size,0),COALESCE(materialized_width,0),COALESCE(materialized_height,0),materialization_state FROM image_candidates WHERE id=$1`, id).Scan(&asset.ID, &asset.ObjectKey, &asset.MediaType, &asset.Checksum, &asset.ByteSize, &asset.Width, &asset.Height, &state)
	if err == pgx.ErrNoRows {
		return Asset{}, nil, ErrNotFound
	}
	if err != nil {
		return Asset{}, nil, err
	}
	if state != "ready" || asset.ObjectKey == "" {
		return Asset{}, nil, ErrNotReady
	}
	body, err := s.runtime.Blobs.Get(ctx, asset.ObjectKey)
	if err != nil {
		if errors.Is(err, blobstore.ErrNotFound) {
			_, _ = s.runtime.DB.Exec(context.WithoutCancel(ctx), `UPDATE image_candidates SET materialization_state='pending',object_key=NULL,blob_checksum=NULL,media_type=NULL,byte_size=NULL,materialized_at=NULL,materialized_width=NULL,materialized_height=NULL WHERE id=$1`, id)
			return Asset{}, nil, ErrNotReady
		}
		return Asset{}, nil, err
	}
	trackAccess(ctx, s.runtime, id)
	return asset, body, nil
}

func (s *Service) ReadVariant(ctx context.Context, id, format string, requestedWidth int) (Asset, []byte, error) {
	if format != "webp" && format != "avif" {
		return Asset{}, nil, ErrNotFound
	}
	var asset Asset
	var candidateState string
	err := s.runtime.DB.QueryRow(ctx, `
		SELECT v.image_id,v.object_key,v.media_type,v.checksum,v.byte_size,v.width,v.height,c.materialization_state
		FROM image_candidates c
		JOIN LATERAL (
			SELECT * FROM image_variants
			WHERE image_id=c.id AND transform_version=$3 AND format=$2
			ORDER BY ABS(width-$4),width LIMIT 1
		) v ON true
		WHERE c.id=$1`, id, format, TransformVersion, requestedWidth).Scan(&asset.ID, &asset.ObjectKey, &asset.MediaType, &asset.Checksum, &asset.ByteSize, &asset.Width, &asset.Height, &candidateState)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if checkErr := s.runtime.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM image_candidates WHERE id=$1)`, id).Scan(&exists); checkErr != nil {
			return Asset{}, nil, checkErr
		}
		if !exists {
			return Asset{}, nil, ErrNotFound
		}
		return Asset{}, nil, ErrNotReady
	}
	if err != nil {
		return Asset{}, nil, err
	}
	if candidateState != "ready" {
		return Asset{}, nil, ErrNotReady
	}
	body, err := s.runtime.Blobs.Get(ctx, asset.ObjectKey)
	if err != nil {
		if errors.Is(err, blobstore.ErrNotFound) {
			_, _ = s.runtime.DB.Exec(context.WithoutCancel(ctx), `DELETE FROM image_variants WHERE image_id=$1`, id)
			_, _ = s.runtime.DB.Exec(context.WithoutCancel(ctx), `UPDATE image_candidates SET materialization_state='pending' WHERE id=$1`, id)
			return Asset{}, nil, ErrNotReady
		}
		return Asset{}, nil, err
	}
	trackAccess(ctx, s.runtime, id)
	return asset, body, nil
}

func (s *Service) Candidates(ctx context.Context, entityID, class string) ([]EntityImageCandidate, error) {
	rows, err := s.runtime.DB.Query(ctx, `
		SELECT id,class,COALESCE(language,''),COALESCE(country,''),
		       COALESCE(width,0),COALESCE(height,0),provider,
		       COALESCE(provider_score,0),materialization_state
		FROM image_candidates
		WHERE entity_id=$1 AND ownership_scope='entity' AND ($2='' OR class=$2)
		ORDER BY class,id
		LIMIT 2000`, entityID, strings.ToLower(strings.TrimSpace(class)))
	if err != nil {
		return nil, fmt.Errorf("list entity image candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]EntityImageCandidate, 0)
	for rows.Next() {
		var candidate EntityImageCandidate
		if err := rows.Scan(&candidate.ID, &candidate.Class, &candidate.Language, &candidate.Country, &candidate.Width, &candidate.Height, &candidate.Provider, &candidate.ProviderScore, &candidate.MaterializationState); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return candidates, nil
}

func validateSourceURL(value *url.URL, allowHTTP bool) error {
	if value == nil || value.Hostname() == "" {
		return fmt.Errorf("image source must be an absolute URL")
	}
	if value.User != nil {
		return fmt.Errorf("image source credentials are forbidden")
	}
	if value.Scheme != "https" && !(allowHTTP && value.Scheme == "http") {
		return fmt.Errorf("image source must use HTTPS")
	}
	host := strings.ToLower(value.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("local image hosts are forbidden")
	}
	if address := net.ParseIP(host); address != nil && (address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsUnspecified()) {
		return fmt.Errorf("private image addresses are forbidden")
	}
	return nil
}
func providerHostAllowed(provider, host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	allowed := map[string][]string{
		"tmdb":            {"image.tmdb.org"},
		"tvdb":            {"artworks.thetvdb.com"},
		"tvmaze":          {"static.tvmaze.com"},
		"anidb":           {"cdn-eu.anidb.net"},
		"fanart":          {"assets.fanart.tv"},
		"coverartarchive": {"coverartarchive.org", "archive.org"},
		"discogs":         {"i.discogs.com", "st.discogs.com"},
		"deezer":          {"cdn-images.dzcdn.net"},
		"lastfm":          {"lastfm.freetls.fastly.net"},
		"wikidata":        {"commons.wikimedia.org", "upload.wikimedia.org"},
		"openlibrary":     {"covers.openlibrary.org", "archive.org"},
		"googlebooks":     {"books.google.com", "books.googleusercontent.com"},
		"kitsu":           {"media.kitsu.app"},
		"myanimelist":     {"api-cdn.myanimelist.net"},
		"apple":           {"mzstatic.com"},
	}
	for _, suffix := range allowed[strings.ToLower(provider)] {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}
func normalizedImageType(value string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0])) {
	case "image/jpeg":
		return "image/jpeg"
	case "image/png":
		return "image/png"
	case "image/gif":
		return "image/gif"
	case "image/webp":
		return "image/webp"
	case "image/avif":
		return "image/avif"
	}
	return ""
}
func imageSuffix(mediaType string) string {
	return map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp", "image/avif": ".avif"}[mediaType]
}
