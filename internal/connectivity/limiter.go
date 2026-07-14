package connectivity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const limiterPrefix = "heya:connectivity:"

var (
	rateScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return {count, redis.call('PTTL', KEYS[1])}
`)
	releaseScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)
)

type memoryRate struct {
	count   int
	expires time.Time
}

// Limiter uses Redis when available so limits hold across API replicas. The
// in-memory implementation exists for dependency-free contract servers and
// unit tests and has the same fixed-window semantics.
type Limiter struct {
	redis *redis.Client
	now   func() time.Time

	mutex    sync.Mutex
	rates    map[string]memoryRate
	inFlight map[string]time.Time
}

func NewLimiter(client *redis.Client) *Limiter {
	return &Limiter{
		redis:    client,
		now:      time.Now,
		rates:    map[string]memoryRate{},
		inFlight: map[string]time.Time{},
	}
}

// Allow consumes one fixed-window request token.
func (limiter *Limiter) Allow(ctx context.Context, scope, source string, limit int, window time.Duration) (bool, int, error) {
	key := limiterKey("rate:"+scope, source)
	if limiter.redis == nil {
		return limiter.allowMemory(key, limit, window)
	}
	result, err := rateScript.Run(ctx, limiter.redis, []string{key}, window.Milliseconds()).Slice()
	if err != nil {
		return false, 0, fmt.Errorf("apply connectivity rate limit: %w", err)
	}
	if len(result) != 2 {
		return false, 0, fmt.Errorf("apply connectivity rate limit: unexpected Redis response")
	}
	count, ok := result[0].(int64)
	if !ok {
		return false, 0, fmt.Errorf("apply connectivity rate limit: invalid count")
	}
	ttlMilliseconds, ok := result[1].(int64)
	if !ok {
		return false, 0, fmt.Errorf("apply connectivity rate limit: invalid TTL")
	}
	retry := secondsCeiling(time.Duration(max(ttlMilliseconds, 0)) * time.Millisecond)
	return count <= int64(limit), retry, nil
}

// Acquire reserves the per-source probe slot. The returned release function
// only removes its own Redis lock, so an expired lock can never be deleted by
// an older request after a newer request has acquired it.
func (limiter *Limiter) Acquire(ctx context.Context, source string, ttl time.Duration) (func(context.Context), bool, int, error) {
	key := limiterKey("inflight", source)
	if limiter.redis == nil {
		return limiter.acquireMemory(key, ttl)
	}
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, false, 0, fmt.Errorf("create connectivity lock token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	acquired, err := limiter.redis.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return nil, false, 0, fmt.Errorf("acquire connectivity lock: %w", err)
	}
	if !acquired {
		remaining, ttlErr := limiter.redis.PTTL(ctx, key).Result()
		if ttlErr != nil && ttlErr != redis.Nil {
			return nil, false, 0, fmt.Errorf("read connectivity lock TTL: %w", ttlErr)
		}
		return nil, false, max(secondsCeiling(remaining), 1), nil
	}
	release := func(releaseCtx context.Context) {
		_, _ = releaseScript.Run(releaseCtx, limiter.redis, []string{key}, token).Result()
	}
	return release, true, 0, nil
}

func (limiter *Limiter) allowMemory(key string, limit int, window time.Duration) (bool, int, error) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()
	now := limiter.now()
	entry := limiter.rates[key]
	if !entry.expires.After(now) {
		entry = memoryRate{expires: now.Add(window)}
	}
	entry.count++
	limiter.rates[key] = entry
	return entry.count <= limit, secondsCeiling(entry.expires.Sub(now)), nil
}

func (limiter *Limiter) acquireMemory(key string, ttl time.Duration) (func(context.Context), bool, int, error) {
	limiter.mutex.Lock()
	now := limiter.now()
	if expires := limiter.inFlight[key]; expires.After(now) {
		limiter.mutex.Unlock()
		return nil, false, max(secondsCeiling(expires.Sub(now)), 1), nil
	}
	expires := now.Add(ttl)
	limiter.inFlight[key] = expires
	limiter.mutex.Unlock()

	release := func(context.Context) {
		limiter.mutex.Lock()
		if limiter.inFlight[key] == expires {
			delete(limiter.inFlight, key)
		}
		limiter.mutex.Unlock()
	}
	return release, true, 0, nil
}

func limiterKey(scope, source string) string {
	digest := sha256.Sum256([]byte(source))
	return limiterPrefix + scope + ":" + hex.EncodeToString(digest[:16])
}

func secondsCeiling(duration time.Duration) int {
	if duration <= 0 {
		return 0
	}
	return int(math.Ceil(duration.Seconds()))
}
