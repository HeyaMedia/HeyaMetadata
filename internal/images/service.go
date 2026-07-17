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
	"os"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/blobstore"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/jackc/pgx/v5"
)

const (
	// Sources at or below MaxOriginalBytes are retained byte-for-byte. Larger
	// sources are streamed through the bounded WebP transform below instead of
	// being held in every image worker's heap.
	MaxOriginalBytes         int64 = 25 * 1024 * 1024
	MaxSourceDownloadBytes   int64 = 100 * 1024 * 1024
	OversizedSquareEdge            = 1200
	OversizedLandscapeWidth        = 1920
	OversizedLandscapeHeight       = 1080
	OversizedPortraitWidth         = 1080
	OversizedPortraitHeight        = 1920
)

// A decoded 60 megapixel image can itself occupy hundreds of MiB. Keep the
// exceptional oversized transform path serial even when the regular image
// queue has many workers.
var oversizedTransformSlot = make(chan struct{}, 1)

var (
	ErrNotFound       = errors.New("image not found")
	ErrNotReady       = errors.New("image is not materialized")
	ErrInProgress     = errors.New("image materialization is already in progress")
	ErrSourceTooLarge = errors.New("image source exceeds the byte limit")
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
	return &Service{runtime: runtime, client: safeHTTPClient(runtime.Config.Worker.ImageMaxWorkers)}
}

func safeHTTPClient(imageMaxWorkers int) *http.Client {
	if imageMaxWorkers < 1 {
		imageMaxWorkers = 1
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ForceAttemptHTTP2: true, MaxIdleConns: imageMaxWorkers * 2, MaxIdleConnsPerHost: imageMaxWorkers, IdleConnTimeout: 30 * time.Second}}
}

func (s *Service) Materialize(ctx context.Context, id string) (asset Asset, returnErr error) {
	asset, _, err := s.ensureOriginal(ctx, id)
	return asset, err
}

// ensureOriginal makes the upstream bytes durable and exposes the original as
// soon as that succeeds. Derived variants are deliberately not part of this
// transaction: a client asking for one WebP must not wait for every size that
// might be useful later.
func (s *Service) ensureOriginal(ctx context.Context, id string) (asset Asset, body []byte, returnErr error) {
	var provider, sourceURL string
	err := s.runtime.DB.QueryRow(ctx, `
		UPDATE image_candidates candidate
		SET materialization_state='working',materialization_error=NULL,materialization_attempted_at=now()
		WHERE candidate.id=$1
		  AND candidate.materialization_state IN ('pending','failed')
		RETURNING provider,source_url`, id).Scan(&provider, &sourceURL)
	if err == pgx.ErrNoRows {
		var state string
		readErr := s.runtime.DB.QueryRow(ctx, `SELECT materialization_state FROM image_candidates WHERE id=$1`, id).Scan(&state)
		if readErr == pgx.ErrNoRows {
			return Asset{}, nil, ErrNotFound
		}
		if readErr != nil {
			return Asset{}, nil, readErr
		}
		if state == "ready" {
			existing, existingBody, readErr := s.Read(ctx, id)
			return existing, existingBody, readErr
		}
		return Asset{}, nil, ErrInProgress
	}
	if err != nil {
		return Asset{}, nil, fmt.Errorf("claim image candidate: %w", err)
	}
	defer func() {
		if returnErr != nil {
			_, _ = s.runtime.DB.Exec(context.WithoutCancel(ctx), `UPDATE image_candidates SET materialization_state='failed',materialization_error=$2 WHERE id=$1`, id, returnErr.Error())
		}
	}()
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return Asset{}, nil, fmt.Errorf("parse image source: %w", err)
	}
	if err := validateSourceURL(parsed, s.allowHTTP); err != nil {
		return Asset{}, nil, err
	}
	if !providerHostAllowed(provider, parsed.Hostname()) {
		return Asset{}, nil, fmt.Errorf("image host %q is not allowed for provider %s", parsed.Hostname(), provider)
	}
	body, mediaType, err := s.fetchSource(ctx, provider, parsed)
	if err != nil {
		return Asset{}, nil, err
	}
	sourceWidth, sourceHeight, err := inspectImage(body)
	if err != nil {
		return Asset{}, nil, err
	}
	digest := sha256.Sum256(body)
	checksum := hex.EncodeToString(digest[:])
	objectKey, err := s.runtime.Blobs.ContentKeyUnder("images/original", checksum, imageSuffix(mediaType))
	if err != nil {
		return Asset{}, nil, err
	}
	if err := registerObject(ctx, s.runtime, objectKey, mediaType, int64(len(body))); err != nil {
		return Asset{}, nil, err
	}
	if err := s.runtime.Blobs.PutImmutable(ctx, objectKey, body, mediaType, ""); err != nil {
		return Asset{}, nil, err
	}
	asset = Asset{ID: id, ObjectKey: objectKey, MediaType: mediaType, Checksum: checksum, ByteSize: int64(len(body)), Width: sourceWidth, Height: sourceHeight}
	if _, err := s.runtime.DB.Exec(ctx, `UPDATE image_candidates SET materialization_state='ready',blob_checksum=$2,object_key=$3,media_type=$4,byte_size=$5,materialization_error=NULL,materialized_at=now(),last_accessed_at=now(),materialized_width=$6,materialized_height=$7,evicted_at=NULL WHERE id=$1`, id, checksum, objectKey, mediaType, len(body), sourceWidth, sourceHeight); err != nil {
		return Asset{}, nil, fmt.Errorf("finish image materialization: %w", err)
	}
	return asset, body, nil
}

func (s *Service) fetchSource(ctx context.Context, provider string, source *url.URL) ([]byte, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.String(), nil)
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("Accept", "image/webp,image/png,image/jpeg,image/gif;q=0.8")
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
		return nil, "", fmt.Errorf("fetch image: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", fmt.Errorf("image source returned HTTP %d", response.StatusCode)
	}
	if declared := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0])); declared != "" && !strings.HasPrefix(declared, "image/") {
		return nil, "", fmt.Errorf("source declared non-image content type %s", declared)
	}
	if response.ContentLength > MaxSourceDownloadBytes {
		return nil, "", fmt.Errorf("image exceeds %d byte hard limit: %w", MaxSourceDownloadBytes, ErrSourceTooLarge)
	}
	if response.ContentLength > MaxOriginalBytes {
		return transcodeOversizedSource(ctx, nil, response.Body)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxOriginalBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read image: %w", err)
	}
	if int64(len(body)) > MaxOriginalBytes {
		return transcodeOversizedSource(ctx, body, response.Body)
	}
	mediaType := normalizedImageType(http.DetectContentType(body))
	if mediaType == "" {
		return nil, "", fmt.Errorf("source is not a supported image")
	}
	return body, mediaType, nil
}

func transcodeOversizedSource(ctx context.Context, prefix []byte, remainder io.Reader) ([]byte, string, error) {
	temporary, err := os.CreateTemp("", "heya-image-source-*")
	if err != nil {
		return nil, "", fmt.Errorf("create oversized image spool: %w", err)
	}
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
	}()
	if len(prefix) > 0 {
		if _, err := temporary.Write(prefix); err != nil {
			return nil, "", fmt.Errorf("spool oversized image prefix: %w", err)
		}
	}
	remainingLimit := MaxSourceDownloadBytes - int64(len(prefix)) + 1
	if remainingLimit < 1 {
		return nil, "", fmt.Errorf("image exceeds %d byte hard limit: %w", MaxSourceDownloadBytes, ErrSourceTooLarge)
	}
	written, err := io.Copy(temporary, io.LimitReader(remainder, remainingLimit))
	if err != nil {
		return nil, "", fmt.Errorf("spool oversized image: %w", err)
	}
	if int64(len(prefix))+written > MaxSourceDownloadBytes {
		return nil, "", fmt.Errorf("image exceeds %d byte hard limit: %w", MaxSourceDownloadBytes, ErrSourceTooLarge)
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		return nil, "", fmt.Errorf("rewind oversized image: %w", err)
	}
	select {
	case oversizedTransformSlot <- struct{}{}:
		defer func() { <-oversizedTransformSlot }()
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
	variant, err := buildStoredWebP(temporary)
	if err != nil {
		return nil, "", fmt.Errorf("transform oversized image: %w", err)
	}
	return variant.Body, variant.MediaType, nil
}

func (s *Service) MaterializeVariant(ctx context.Context, id string, requestedWidth int) (Asset, error) {
	if requestedWidth < 1 || requestedWidth > 3840 {
		return Asset{}, fmt.Errorf("image variant width must be between 1 and 3840")
	}
	requestedWidth = CanonicalVariantWidth(requestedWidth)
	if existing, _, err := s.ReadVariant(ctx, id, requestedWidth); err == nil {
		return existing, nil
	} else if errors.Is(err, ErrNotFound) {
		return Asset{}, err
	} else if !errors.Is(err, ErrNotReady) {
		return Asset{}, err
	}
	original, body, err := s.ensureOriginal(ctx, id)
	if err != nil {
		return Asset{}, err
	}
	if existing, _, err := s.ReadVariant(ctx, id, requestedWidth); err == nil {
		return existing, nil
	} else if errors.Is(err, ErrNotFound) {
		return Asset{}, err
	} else if !errors.Is(err, ErrNotReady) {
		return Asset{}, err
	}
	variant, err := buildVariant(body, min(requestedWidth, original.Width))
	if err != nil {
		return Asset{}, err
	}
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
	if _, err := s.runtime.DB.Exec(ctx, `
		INSERT INTO image_variants(image_id,transform_version,format,width,height,checksum,object_key,media_type,byte_size)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT(image_id,transform_version,format,width) DO UPDATE SET
			height=EXCLUDED.height,checksum=EXCLUDED.checksum,object_key=EXCLUDED.object_key,
			media_type=EXCLUDED.media_type,byte_size=EXCLUDED.byte_size`,
		id, TransformVersion, variant.Format, variant.Width, variant.Height, variant.Checksum, variant.ObjectKey, variant.MediaType, variant.ByteSize); err != nil {
		return Asset{}, fmt.Errorf("record image variant: %w", err)
	}
	trackAccess(ctx, s.runtime, id)
	return Asset{ID: id, ObjectKey: variant.ObjectKey, MediaType: variant.MediaType, Checksum: variant.Checksum, ByteSize: variant.ByteSize, Width: variant.Width, Height: variant.Height}, nil
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

func (s *Service) ReadVariant(ctx context.Context, id string, requestedWidth int) (Asset, []byte, error) {
	requestedWidth = CanonicalVariantWidth(requestedWidth)
	var asset Asset
	err := s.runtime.DB.QueryRow(ctx, `
		SELECT v.image_id,v.object_key,v.media_type,v.checksum,v.byte_size,v.width,v.height
		FROM image_candidates c
		JOIN image_variants v ON v.image_id=c.id
			AND v.transform_version=$3
			AND v.format=$2
			AND v.width=LEAST($4,c.materialized_width)
		WHERE c.id=$1 AND c.materialization_state='ready'`, id, VariantFormat, TransformVersion, requestedWidth).Scan(&asset.ID, &asset.ObjectKey, &asset.MediaType, &asset.Checksum, &asset.ByteSize, &asset.Width, &asset.Height)
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
	body, err := s.runtime.Blobs.Get(ctx, asset.ObjectKey)
	if err != nil {
		if errors.Is(err, blobstore.ErrNotFound) {
			_, _ = s.runtime.DB.Exec(context.WithoutCancel(ctx), `DELETE FROM image_variants WHERE image_id=$1 AND transform_version=$2 AND format=$3 AND width=$4`, id, TransformVersion, VariantFormat, asset.Width)
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
		"audiodb":         {"theaudiodb.com"},
		"bandcamp":        {"bcbits.com"},
		"tidal":           {"resources.tidal.com"},
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
	}
	return ""
}
func imageSuffix(mediaType string) string {
	return map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp"}[mediaType]
}
