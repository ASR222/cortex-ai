package gateway

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter is a fixed-window counter in Redis, shared by all gateway
// replicas.
//
// Fixed windows allow up to 2x the limit across a window boundary; that is an
// acceptable trade for being one INCR per request. A sliding-window log or
// token bucket would be the next step if that mattered.
type RateLimiter struct {
	Redis *redis.Client
}

// Limit describes one rule, e.g. 10 requests per minute.
type Limit struct {
	Name   string
	Max    int64
	Window time.Duration
}

// Allow counts one hit for key and reports whether it is within the limit,
// plus how long until the window resets.
func (l *RateLimiter) Allow(ctx context.Context, lim Limit, key string) (bool, time.Duration, error) {
	now := time.Now()
	window := now.Truncate(lim.Window)
	rkey := fmt.Sprintf("rl:%s:%s:%d", lim.Name, key, window.Unix())

	pipe := l.Redis.TxPipeline()
	incr := pipe.Incr(ctx, rkey)
	pipe.Expire(ctx, rkey, lim.Window+time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, 0, err
	}
	retry := window.Add(lim.Window).Sub(now)
	return incr.Val() <= lim.Max, retry, nil
}
