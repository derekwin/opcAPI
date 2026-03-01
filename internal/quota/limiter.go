package quota

import (
	"context"
	"fmt"
	"sync"
	"time"

	redisstore "github.com/derekwin/opcAPI/internal/store/redis"
)

type RequestDecision struct {
	Allowed       bool
	Limit         int
	Remaining     int
	RetryAfterSec int
	Reason        string
}

type TokenDecision struct {
	Allowed   bool
	Limit     int
	Remaining int
	Reason    string
}

type KeyPolicy struct {
	RPMLimit        int
	DailyTokenLimit int
}

type minuteCounter struct {
	bucket time.Time
	count  int
}

type dailyTokenCounter struct {
	day  string
	used int
}

type Limiter struct {
	defaultRPMLimit        int
	defaultDailyTokenLimit int

	redisClient *redisstore.Client
	redisPrefix string

	mu            sync.RWMutex
	reqCounters   map[string]minuteCounter
	tokenCounters map[string]dailyTokenCounter
	policies      map[string]KeyPolicy

	nowFn func() time.Time
}

func NewLimiter(rpmLimit, dailyTokenLimit int) *Limiter {
	return &Limiter{
		defaultRPMLimit:        rpmLimit,
		defaultDailyTokenLimit: dailyTokenLimit,
		reqCounters:            make(map[string]minuteCounter),
		tokenCounters:          make(map[string]dailyTokenCounter),
		policies:               make(map[string]KeyPolicy),
		nowFn:                  time.Now,
	}
}

func NewRedisLimiter(rpmLimit, dailyTokenLimit int, client *redisstore.Client, prefix string) *Limiter {
	l := NewLimiter(rpmLimit, dailyTokenLimit)
	if prefix == "" {
		prefix = "opcapi:quota"
	}
	l.redisClient = client
	l.redisPrefix = prefix
	return l
}

func (l *Limiter) SetKeyPolicies(policies map[string]KeyPolicy) {
	if l == nil {
		return
	}

	cloned := make(map[string]KeyPolicy, len(policies))
	for k, v := range policies {
		if k == "" {
			continue
		}
		cloned[k] = v
	}

	l.mu.Lock()
	l.policies = cloned
	l.mu.Unlock()
}

func (l *Limiter) AllowRequest(key string) RequestDecision {
	if l == nil {
		return RequestDecision{Allowed: true}
	}
	if l.redisClient != nil {
		return l.allowRequestRedis(key)
	}
	return l.allowRequestMemory(key)
}

func (l *Limiter) AllowTokenUsage(key string) TokenDecision {
	if l == nil {
		return TokenDecision{Allowed: true}
	}
	if l.redisClient != nil {
		return l.allowTokenUsageRedis(key)
	}
	return l.allowTokenUsageMemory(key)
}

func (l *Limiter) AddTokenUsage(key string, tokens int) {
	if l == nil {
		return
	}
	if l.redisClient != nil {
		l.addTokenUsageRedis(key, tokens)
		return
	}
	l.addTokenUsageMemory(key, tokens)
}

func (l *Limiter) allowRequestMemory(key string) RequestDecision {
	rpmLimit := l.resolveRPMLimit(key)
	if rpmLimit <= 0 {
		return RequestDecision{Allowed: true}
	}

	now := l.nowUTC()
	bucket := now.Truncate(time.Minute)

	l.mu.Lock()
	defer l.mu.Unlock()

	counter := l.reqCounters[key]
	if counter.bucket != bucket {
		counter.bucket = bucket
		counter.count = 0
	}

	if counter.count >= rpmLimit {
		retryAfter := int(bucket.Add(time.Minute).Sub(now).Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		l.reqCounters[key] = counter
		return RequestDecision{
			Allowed:       false,
			Limit:         rpmLimit,
			Remaining:     0,
			RetryAfterSec: retryAfter,
			Reason:        "rpm_limit_exceeded",
		}
	}

	counter.count++
	l.reqCounters[key] = counter

	remaining := rpmLimit - counter.count
	if remaining < 0 {
		remaining = 0
	}
	return RequestDecision{
		Allowed:   true,
		Limit:     rpmLimit,
		Remaining: remaining,
	}
}

func (l *Limiter) allowTokenUsageMemory(key string) TokenDecision {
	dailyLimit := l.resolveDailyTokenLimit(key)
	if dailyLimit <= 0 {
		return TokenDecision{Allowed: true}
	}

	today := l.nowUTC().Format("2006-01-02")

	l.mu.Lock()
	defer l.mu.Unlock()

	counter := l.tokenCounters[key]
	if counter.day != today {
		counter.day = today
		counter.used = 0
	}

	if counter.used >= dailyLimit {
		l.tokenCounters[key] = counter
		return TokenDecision{
			Allowed:   false,
			Limit:     dailyLimit,
			Remaining: 0,
			Reason:    "daily_token_limit_exceeded",
		}
	}

	remaining := dailyLimit - counter.used
	if remaining < 0 {
		remaining = 0
	}
	l.tokenCounters[key] = counter
	return TokenDecision{
		Allowed:   true,
		Limit:     dailyLimit,
		Remaining: remaining,
	}
}

func (l *Limiter) addTokenUsageMemory(key string, tokens int) {
	dailyLimit := l.resolveDailyTokenLimit(key)
	if dailyLimit <= 0 || tokens <= 0 {
		return
	}

	today := l.nowUTC().Format("2006-01-02")

	l.mu.Lock()
	defer l.mu.Unlock()

	counter := l.tokenCounters[key]
	if counter.day != today {
		counter.day = today
		counter.used = 0
	}
	counter.used += tokens
	l.tokenCounters[key] = counter
}

func (l *Limiter) allowRequestRedis(key string) RequestDecision {
	rpmLimit := l.resolveRPMLimit(key)
	if rpmLimit <= 0 {
		return RequestDecision{Allowed: true}
	}

	now := l.nowUTC()
	bucket := now.Format("200601021504")
	redisKey := fmt.Sprintf("%s:req:%s:%s", l.redisPrefix, key, bucket)

	count, err := l.redisClient.Incr(context.Background(), redisKey)
	if err != nil {
		return RequestDecision{Allowed: true, Limit: rpmLimit, Remaining: rpmLimit, Reason: "redis_unavailable"}
	}
	if count == 1 {
		_ = l.redisClient.Expire(context.Background(), redisKey, 2*time.Minute)
	}

	if int(count) > rpmLimit {
		retryAfter := int(now.Truncate(time.Minute).Add(time.Minute).Sub(now).Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		return RequestDecision{
			Allowed:       false,
			Limit:         rpmLimit,
			Remaining:     0,
			RetryAfterSec: retryAfter,
			Reason:        "rpm_limit_exceeded",
		}
	}

	remaining := rpmLimit - int(count)
	if remaining < 0 {
		remaining = 0
	}
	return RequestDecision{Allowed: true, Limit: rpmLimit, Remaining: remaining}
}

func (l *Limiter) allowTokenUsageRedis(key string) TokenDecision {
	dailyLimit := l.resolveDailyTokenLimit(key)
	if dailyLimit <= 0 {
		return TokenDecision{Allowed: true}
	}

	day := l.nowUTC().Format("20060102")
	redisKey := fmt.Sprintf("%s:tok:%s:%s", l.redisPrefix, key, day)
	used, ok, err := l.redisClient.GetInt64(context.Background(), redisKey)
	if err != nil {
		return TokenDecision{Allowed: true, Limit: dailyLimit, Remaining: dailyLimit, Reason: "redis_unavailable"}
	}
	if !ok {
		used = 0
	}

	if int(used) >= dailyLimit {
		return TokenDecision{
			Allowed:   false,
			Limit:     dailyLimit,
			Remaining: 0,
			Reason:    "daily_token_limit_exceeded",
		}
	}

	remaining := dailyLimit - int(used)
	if remaining < 0 {
		remaining = 0
	}
	return TokenDecision{Allowed: true, Limit: dailyLimit, Remaining: remaining}
}

func (l *Limiter) addTokenUsageRedis(key string, tokens int) {
	dailyLimit := l.resolveDailyTokenLimit(key)
	if dailyLimit <= 0 || tokens <= 0 {
		return
	}

	day := l.nowUTC().Format("20060102")
	redisKey := fmt.Sprintf("%s:tok:%s:%s", l.redisPrefix, key, day)
	if _, err := l.redisClient.IncrBy(context.Background(), redisKey, int64(tokens)); err != nil {
		return
	}
	_ = l.redisClient.Expire(context.Background(), redisKey, 48*time.Hour)
}

func (l *Limiter) resolveRPMLimit(key string) int {
	if l == nil {
		return 0
	}

	l.mu.RLock()
	defer l.mu.RUnlock()
	if policy, ok := l.policies[key]; ok {
		return policy.RPMLimit
	}
	return l.defaultRPMLimit
}

func (l *Limiter) resolveDailyTokenLimit(key string) int {
	if l == nil {
		return 0
	}

	l.mu.RLock()
	defer l.mu.RUnlock()
	if policy, ok := l.policies[key]; ok {
		return policy.DailyTokenLimit
	}
	return l.defaultDailyTokenLimit
}

func (l *Limiter) nowUTC() time.Time {
	fn := l.nowFn
	if fn == nil {
		fn = time.Now
	}
	return fn().UTC()
}
