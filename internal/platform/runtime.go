package platform

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/blobstore"
	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Runtime struct {
	DB    *pgxpool.Pool
	Redis *redis.Client
	Blobs *blobstore.Store
}

func Open(ctx context.Context, cfg config.Config) (*Runtime, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database configuration: %w", err)
	}
	poolConfig.MaxConns = 20
	poolConfig.MinIdleConns = 1
	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.MaxConnIdleTime = 15 * time.Minute
	database, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("open database pool: %w", err)
	}

	redisOptions, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("parse Redis configuration: %w", err)
	}
	redisOptions.Protocol = 3
	redisOptions.PoolSize = 20
	redisClient := redis.NewClient(redisOptions)

	blobs, err := blobstore.New(ctx, cfg.S3)
	if err != nil {
		redisClient.Close()
		database.Close()
		return nil, err
	}

	return &Runtime{DB: database, Redis: redisClient, Blobs: blobs}, nil
}

func (r *Runtime) Close() {
	if r == nil {
		return
	}
	if r.Redis != nil {
		_ = r.Redis.Close()
	}
	if r.DB != nil {
		r.DB.Close()
	}
}

// Check probes all three platform dependencies concurrently. The returned map
// always contains every dependency so readiness responses do not hide a second
// failure behind the first one encountered.
func (r *Runtime) Check(ctx context.Context) map[string]error {
	checks := map[string]func(context.Context) error{
		"postgres": r.DB.Ping,
		"redis": func(ctx context.Context) error {
			return r.Redis.Ping(ctx).Err()
		},
		"s3": r.Blobs.Check,
	}

	results := make(map[string]error, len(checks))
	var mutex sync.Mutex
	var wait sync.WaitGroup
	for name, check := range checks {
		wait.Add(1)
		go func() {
			defer wait.Done()
			checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			err := check(checkCtx)
			mutex.Lock()
			results[name] = err
			mutex.Unlock()
		}()
	}
	wait.Wait()
	return results
}

func (r *Runtime) Ensure(ctx context.Context, cfg config.Config) error {
	checks := r.Check(ctx)
	if err := checks["postgres"]; err != nil {
		return fmt.Errorf("Postgres is unavailable: %w", err)
	}
	if err := checks["redis"]; err != nil {
		return fmt.Errorf("Redis is unavailable: %w", err)
	}
	if err := r.Blobs.EnsureBucket(ctx, cfg.S3.AutoCreateBucket); err != nil {
		return fmt.Errorf("S3 is unavailable: %w", err)
	}
	return nil
}
