package blobstore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
)

var (
	ErrNotConfigured = errors.New("S3 credentials are not configured")
	checksumPattern  = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type Store struct {
	endpoint  string
	region    string
	bucket    string
	prefix    string
	accessKey string
	secretKey string
	client    *http.Client
}

func New(_ context.Context, cfg config.S3Config) (*Store, error) {
	return &Store{
		endpoint: strings.TrimRight(cfg.Endpoint, "/"),
		region:   cfg.Region, bucket: cfg.Bucket, prefix: strings.Trim(cfg.Prefix, "/"),
		accessKey: cfg.AccessKeyID, secretKey: cfg.SecretAccessKey,
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: &http.Transport{DisableCompression: true},
		},
	}, nil
}

func (s *Store) Configured() bool {
	return s != nil && s.accessKey != "" && s.secretKey != ""
}

func (s *Store) Check(ctx context.Context) error {
	if !s.Configured() {
		return ErrNotConfigured
	}
	response, err := s.do(ctx, http.MethodGet, path.Join(s.prefix, ".readiness"), nil, nil)
	if err != nil {
		return fmt.Errorf("probe S3 prefix %q in bucket %q: %w", s.prefix, s.bucket, err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if response.StatusCode == http.StatusNotFound && strings.Contains(string(body), "NoSuchKey") {
		return nil
	}
	return s.statusError("probe", response.StatusCode, body)
}

func (s *Store) EnsureBucket(ctx context.Context, create bool) error {
	if err := s.Check(ctx); err == nil {
		return nil
	} else if !create {
		return err
	}

	response, err := s.do(ctx, http.MethodPut, "", nil, nil)
	if err != nil {
		return fmt.Errorf("create S3 bucket %q: %w", s.bucket, err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 || response.StatusCode == http.StatusConflict {
		return s.Check(ctx)
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	return s.statusError("create bucket", response.StatusCode, body)
}

// PutImmutable writes an object only when the key does not already exist.
// Content-addressed callers may treat a precondition failure as success because
// the key uniquely identifies the bytes they intended to store.
func (s *Store) PutImmutable(ctx context.Context, key string, body []byte, mediaType, contentEncoding string) error {
	if !s.Configured() {
		return ErrNotConfigured
	}
	headers := http.Header{
		"Content-Type":  []string{mediaType},
		"If-None-Match": []string{"*"},
	}
	if contentEncoding != "" {
		headers.Set("Content-Encoding", contentEncoding)
	}
	response, err := s.do(ctx, http.MethodPut, key, body, headers)
	if err != nil {
		return fmt.Errorf("put immutable S3 object %q: %w", key, err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 ||
		response.StatusCode == http.StatusConflict || response.StatusCode == http.StatusPreconditionFailed {
		return nil
	}
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	return s.statusError("put object", response.StatusCode, responseBody)
}

func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	if !s.Configured() {
		return nil, ErrNotConfigured
	}
	response, err := s.do(ctx, http.MethodGet, key, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("get S3 object %q: %w", key, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		return nil, s.statusError("get object", response.StatusCode, responseBody)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read S3 object %q: %w", key, err)
	}
	return body, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	if !s.Configured() {
		return ErrNotConfigured
	}
	response, err := s.do(ctx, http.MethodDelete, key, nil, nil)
	if err != nil {
		return fmt.Errorf("delete S3 object %q: %w", key, err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 || response.StatusCode == http.StatusNotFound {
		return nil
	}
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	return s.statusError("delete object", response.StatusCode, responseBody)
}

func (s *Store) ContentKey(checksum, suffix string) (string, error) {
	return ContentKey(s.prefix, checksum, suffix)
}

func (s *Store) ContentKeyUnder(objectPrefix, checksum, suffix string) (string, error) {
	return ContentKey(path.Join(s.prefix, strings.Trim(objectPrefix, "/")), checksum, suffix)
}

func ContentKey(prefix, checksum, suffix string) (string, error) {
	if !checksumPattern.MatchString(checksum) {
		return "", fmt.Errorf("invalid SHA-256 checksum %q", checksum)
	}
	return path.Join(strings.Trim(prefix, "/"), "blobs", "sha256", checksum[:2], checksum[2:4], checksum+suffix), nil
}

func (s *Store) do(ctx context.Context, method, key string, payload []byte, headers http.Header) (*http.Response, error) {
	requestURL := s.endpoint + "/" + s.bucket
	if key != "" {
		requestURL += "/" + strings.TrimLeft(key, "/")
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build S3 request: %w", err)
	}
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	s.sign(request, payload)
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send S3 request: %w", err)
	}
	return response, nil
}

func (s *Store) sign(request *http.Request, payload []byte) {
	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")
	payloadHash := sha256Hex(payload)
	request.Header.Set("X-Amz-Date", amzDate)
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)
	request.Host = request.URL.Host

	headerNames := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	canonicalHeaders := map[string]string{
		"host": request.URL.Host, "x-amz-content-sha256": payloadHash, "x-amz-date": amzDate,
	}
	for _, name := range []string{"content-encoding", "content-type"} {
		if value := strings.TrimSpace(request.Header.Get(name)); value != "" {
			headerNames = append(headerNames, name)
			canonicalHeaders[name] = value
		}
	}
	sort.Strings(headerNames)
	var canonicalHeaderBlock strings.Builder
	for _, name := range headerNames {
		canonicalHeaderBlock.WriteString(name + ":" + canonicalHeaders[name] + "\n")
	}
	signedHeaders := strings.Join(headerNames, ";")
	canonicalRequest := strings.Join([]string{
		request.Method, request.URL.EscapedPath(), request.URL.Query().Encode(),
		canonicalHeaderBlock.String(), signedHeaders, payloadHash,
	}, "\n")
	scope := dateStamp + "/" + s.region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonicalRequest))
	signature := hex.EncodeToString(hmacSHA256(deriveSigningKey(s.secretKey, dateStamp, s.region), []byte(stringToSign)))
	request.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.accessKey, scope, signedHeaders, signature,
	))
}

func (s *Store) statusError(operation string, status int, body []byte) error {
	code := "unknown"
	if start := strings.Index(string(body), "<Code>"); start >= 0 {
		rest := string(body)[start+len("<Code>"):]
		if end := strings.Index(rest, "</Code>"); end >= 0 {
			code = rest[:end]
		}
	}
	return fmt.Errorf("S3 %s failed for bucket %q with status %d (%s)", operation, s.bucket, status, code)
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func hmacSHA256(key, data []byte) []byte {
	hash := hmac.New(sha256.New, key)
	_, _ = hash.Write(data)
	return hash.Sum(nil)
}

func deriveSigningKey(secret, date, region string) []byte {
	key := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	key = hmacSHA256(key, []byte(region))
	key = hmacSHA256(key, []byte("s3"))
	return hmacSHA256(key, []byte("aws4_request"))
}
