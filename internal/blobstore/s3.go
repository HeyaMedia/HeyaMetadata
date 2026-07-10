package blobstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

var (
	ErrNotConfigured = errors.New("S3 credentials are not configured")
	checksumPattern  = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type s3Client interface {
	CreateBucket(context.Context, *s3.CreateBucketInput, ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

type Store struct {
	client     s3Client
	bucket     string
	configured bool
}

func New(ctx context.Context, cfg config.S3Config) (*Store, error) {
	configured := cfg.AccessKeyID != "" && cfg.SecretAccessKey != ""
	var credentialsProvider aws.CredentialsProvider = aws.AnonymousCredentials{}
	if configured {
		credentialsProvider = credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentialsProvider),
	)
	if err != nil {
		return nil, fmt.Errorf("load S3 configuration: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(cfg.Endpoint)
		options.UsePathStyle = cfg.PathStyle
	})
	return &Store{client: client, bucket: cfg.Bucket, configured: configured}, nil
}

func (s *Store) Configured() bool {
	return s != nil && s.configured
}

func (s *Store) Check(ctx context.Context) error {
	if !s.Configured() {
		return ErrNotConfigured
	}
	if _, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)}); err != nil {
		return fmt.Errorf("head S3 bucket %q: %w", s.bucket, err)
	}
	return nil
}

func (s *Store) EnsureBucket(ctx context.Context, create bool) error {
	if !s.Configured() {
		return ErrNotConfigured
	}
	if err := s.Check(ctx); err == nil {
		return nil
	} else if !create {
		return err
	}

	_, err := s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.bucket)})
	if err != nil && !hasErrorCode(err, "BucketAlreadyExists", "BucketAlreadyOwnedByYou") {
		return fmt.Errorf("create S3 bucket %q: %w", s.bucket, err)
	}
	return s.Check(ctx)
}

// PutImmutable writes an object only when the key does not already exist.
// Content-addressed callers may treat a precondition failure as success because
// the key uniquely identifies the bytes they intended to store.
func (s *Store) PutImmutable(ctx context.Context, key string, body []byte, mediaType, contentEncoding string) error {
	if !s.Configured() {
		return ErrNotConfigured
	}
	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String(mediaType),
		IfNoneMatch: aws.String("*"),
	}
	if contentEncoding != "" {
		input.ContentEncoding = aws.String(contentEncoding)
	}
	if _, err := s.client.PutObject(ctx, input); err != nil && !hasErrorCode(err, "PreconditionFailed", "ConditionalRequestConflict") {
		return fmt.Errorf("put immutable S3 object %q: %w", key, err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	if !s.Configured() {
		return nil, ErrNotConfigured
	}
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get S3 object %q: %w", key, err)
	}
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("read S3 object %q: %w", key, err)
	}
	return body, nil
}

func ContentKey(checksum, suffix string) (string, error) {
	if !checksumPattern.MatchString(checksum) {
		return "", fmt.Errorf("invalid SHA-256 checksum %q", checksum)
	}
	return fmt.Sprintf("blobs/sha256/%s/%s/%s%s", checksum[:2], checksum[2:4], checksum, suffix), nil
}

func hasErrorCode(err error, codes ...string) bool {
	var apiError smithy.APIError
	if !errors.As(err, &apiError) {
		return false
	}
	for _, code := range codes {
		if apiError.ErrorCode() == code {
			return true
		}
	}
	return false
}
