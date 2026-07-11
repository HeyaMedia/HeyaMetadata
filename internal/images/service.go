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
	ID, ObjectKey, MediaType string
	ByteSize                 int64
}

func NewService(runtime *platform.Runtime) *Service {
	return &Service{runtime: runtime, client: safeHTTPClient()}
}

func safeHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ForceAttemptHTTP2: true, MaxIdleConns: 20, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second}}
}

func (s *Service) Materialize(ctx context.Context, id string) (asset Asset, returnErr error) {
	var provider, sourceURL, state string
	err := s.runtime.DB.QueryRow(ctx, `UPDATE image_candidates SET materialization_state='working',materialization_error=NULL WHERE id=$1 AND materialization_state IN ('pending','failed') RETURNING provider,source_url,materialization_state`, id).Scan(&provider, &sourceURL, &state)
	if err == pgx.ErrNoRows {
		var existing Asset
		var existingState string
		readErr := s.runtime.DB.QueryRow(ctx, `SELECT id,COALESCE(object_key,''),COALESCE(media_type,''),COALESCE(byte_size,0),materialization_state FROM image_candidates WHERE id=$1`, id).Scan(&existing.ID, &existing.ObjectKey, &existing.MediaType, &existing.ByteSize, &existingState)
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
	if err := s.runtime.Blobs.PutImmutable(ctx, objectKey, body, mediaType, ""); err != nil {
		return Asset{}, err
	}
	asset = Asset{ID: id, ObjectKey: objectKey, MediaType: mediaType, ByteSize: int64(len(body))}
	if _, err := s.runtime.DB.Exec(ctx, `UPDATE image_candidates SET materialization_state='ready',blob_checksum=$2,object_key=$3,media_type=$4,byte_size=$5,materialization_error=NULL,materialized_at=now() WHERE id=$1`, id, checksum, objectKey, mediaType, len(body)); err != nil {
		return Asset{}, fmt.Errorf("finish image materialization: %w", err)
	}
	return asset, nil
}

func (s *Service) Read(ctx context.Context, id string) (Asset, []byte, error) {
	var asset Asset
	var state string
	err := s.runtime.DB.QueryRow(ctx, `SELECT id,COALESCE(object_key,''),COALESCE(media_type,''),COALESCE(byte_size,0),materialization_state FROM image_candidates WHERE id=$1`, id).Scan(&asset.ID, &asset.ObjectKey, &asset.MediaType, &asset.ByteSize, &state)
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
		return Asset{}, nil, err
	}
	return asset, body, nil
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
	allowed := map[string][]string{"tmdb": {"image.tmdb.org"}, "tvdb": {"artworks.thetvdb.com"}, "fanart": {"assets.fanart.tv"}, "discogs": {"i.discogs.com", "st.discogs.com"}, "deezer": {"cdn-images.dzcdn.net"}, "lastfm": {"lastfm.freetls.fastly.net"}, "wikidata": {"commons.wikimedia.org", "upload.wikimedia.org"}}
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
