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
	Allowed           bool
	Limit             int
	Remaining         int
	FiveHourLimit     int
	FiveHourRemaining int
	MonthlyLimit      int
	MonthlyRemaining  int
	Reason            string
}

type KeyPolicy struct {
	RPMLimit           int
	DailyTokenLimit    int // legacy
	FiveHourTokenLimit int
	MonthlyTokenLimit  int
}

type minuteCounter struct {
	bucket time.Time
	count  int
}

type dailyTokenCounter struct {
	day  string
	used int
}

type fiveHourTokenCounter struct {
	window string
	used   int
}

type monthlyTokenCounter struct {
	month string
	used  int
}

type Limiter struct {
	defaultRPMLimit        int
	defaultDailyTokenLimit int

	redisClient *redisstore.Client
	redisPrefix string

	mu            sync.RWMutex
	reqCounters   map[string]minuteCounter
	tokenCounters map[string]dailyTokenCounter
	fiveHour      map[string]fiveHourTokenCounter
	monthly       map[string]monthlyTokenCounter
	policies      map[string]KeyPolicy

	nowFn func() time.Time
}

func NewLimiter(rpmLimit, dailyTokenLimit int) *Limiter {
	return &Limiter{
		defaultRPMLimit:        rpmLimit,
		defaultDailyTokenLimit: dailyTokenLimit,
		reqCounters:            make(map[string]minuteCounter),
		tokenCounters:          make(map[string]dailyTokenCounter),
		fiveHour:               make(map[string]fiveHourTokenCounter),
		monthly:                make(map[string]monthlyTokenCounter),
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

func (l *Limiter) SetKeyPolicy(key string, policy KeyPolicy) {
	if l == nil || key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.policies[key] = policy
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
	fiveHourLimit := l.resolveFiveHourTokenLimit(key)
	monthlyLimit := l.resolveMonthlyTokenLimit(key)
	if dailyLimit <= 0 && fiveHourLimit <= 0 && monthlyLimit <= 0 {
		return TokenDecision{Allowed: true}
	}

	now := l.nowUTC()
	today := now.Format("2006-01-02")
	fiveHourWindow := l.fiveHourBucket(now)
	month := now.Format("200601")

	l.mu.Lock()
	defer l.mu.Unlock()

	counter := l.tokenCounters[key]
	if counter.day != today {
		counter.day = today
		counter.used = 0
	}
	fh := l.fiveHour[key]
	if fh.window != fiveHourWindow {
		fh.window = fiveHourWindow
		fh.used = 0
	}
	mon := l.monthly[key]
	if mon.month != month {
		mon.month = month
		mon.used = 0
	}

	if fiveHourLimit > 0 && fh.used >= fiveHourLimit {
		l.fiveHour[key] = fh
		return TokenDecision{
			Allowed:           false,
			FiveHourLimit:     fiveHourLimit,
			FiveHourRemaining: 0,
			Reason:            "five_hour_token_limit_exceeded",
		}
	}
	if monthlyLimit > 0 && mon.used >= monthlyLimit {
		l.monthly[key] = mon
		return TokenDecision{
			Allowed:          false,
			MonthlyLimit:     monthlyLimit,
			MonthlyRemaining: 0,
			Reason:           "monthly_token_limit_exceeded",
		}
	}
	if dailyLimit > 0 && counter.used >= dailyLimit {
		l.tokenCounters[key] = counter
		return TokenDecision{
			Allowed:   false,
			Limit:     dailyLimit,
			Remaining: 0,
			Reason:    "daily_token_limit_exceeded",
		}
	}

	remaining := 0
	if dailyLimit > 0 {
		remaining = dailyLimit - counter.used
		if remaining < 0 {
			remaining = 0
		}
	}
	fiveHourRemaining := 0
	if fiveHourLimit > 0 {
		fiveHourRemaining = fiveHourLimit - fh.used
		if fiveHourRemaining < 0 {
			fiveHourRemaining = 0
		}
	}
	monthlyRemaining := 0
	if monthlyLimit > 0 {
		monthlyRemaining = monthlyLimit - mon.used
		if monthlyRemaining < 0 {
			monthlyRemaining = 0
		}
	}
	l.tokenCounters[key] = counter
	l.fiveHour[key] = fh
	l.monthly[key] = mon
	return TokenDecision{
		Allowed:           true,
		Limit:             dailyLimit,
		Remaining:         remaining,
		FiveHourLimit:     fiveHourLimit,
		FiveHourRemaining: fiveHourRemaining,
		MonthlyLimit:      monthlyLimit,
		MonthlyRemaining:  monthlyRemaining,
	}
}

func (l *Limiter) addTokenUsageMemory(key string, tokens int) {
	dailyLimit := l.resolveDailyTokenLimit(key)
	fiveHourLimit := l.resolveFiveHourTokenLimit(key)
	monthlyLimit := l.resolveMonthlyTokenLimit(key)
	if (dailyLimit <= 0 && fiveHourLimit <= 0 && monthlyLimit <= 0) || tokens <= 0 {
		return
	}

	now := l.nowUTC()
	today := now.Format("2006-01-02")
	fiveHourWindow := l.fiveHourBucket(now)
	month := now.Format("200601")

	l.mu.Lock()
	defer l.mu.Unlock()

	if dailyLimit > 0 {
		counter := l.tokenCounters[key]
		if counter.day != today {
			counter.day = today
			counter.used = 0
		}
		counter.used += tokens
		l.tokenCounters[key] = counter
	}
	if fiveHourLimit > 0 {
		fh := l.fiveHour[key]
		if fh.window != fiveHourWindow {
			fh.window = fiveHourWindow
			fh.used = 0
		}
		fh.used += tokens
		l.fiveHour[key] = fh
	}
	if monthlyLimit > 0 {
		mon := l.monthly[key]
		if mon.month != month {
			mon.month = month
			mon.used = 0
		}
		mon.used += tokens
		l.monthly[key] = mon
	}
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
	fiveHourLimit := l.resolveFiveHourTokenLimit(key)
	monthlyLimit := l.resolveMonthlyTokenLimit(key)
	if dailyLimit <= 0 && fiveHourLimit <= 0 && monthlyLimit <= 0 {
		return TokenDecision{Allowed: true}
	}

	now := l.nowUTC()
	day := now.Format("20060102")
	month := now.Format("200601")
	fiveHourWindow := l.fiveHourBucket(now)

	dailyUsed := int64(0)
	if dailyLimit > 0 {
		redisKey := fmt.Sprintf("%s:tok:%s:%s", l.redisPrefix, key, day)
		used, ok, err := l.redisClient.GetInt64(context.Background(), redisKey)
		if err != nil {
			return TokenDecision{Allowed: true, Limit: dailyLimit, Remaining: dailyLimit, Reason: "redis_unavailable"}
		}
		if ok {
			dailyUsed = used
		}
	}
	fiveHourUsed := int64(0)
	if fiveHourLimit > 0 {
		redisKey := fmt.Sprintf("%s:tok5h:%s:%s", l.redisPrefix, key, fiveHourWindow)
		used, ok, err := l.redisClient.GetInt64(context.Background(), redisKey)
		if err != nil {
			return TokenDecision{Allowed: true, FiveHourLimit: fiveHourLimit, FiveHourRemaining: fiveHourLimit, Reason: "redis_unavailable"}
		}
		if ok {
			fiveHourUsed = used
		}
	}
	monthlyUsed := int64(0)
	if monthlyLimit > 0 {
		redisKey := fmt.Sprintf("%s:tokm:%s:%s", l.redisPrefix, key, month)
		used, ok, err := l.redisClient.GetInt64(context.Background(), redisKey)
		if err != nil {
			return TokenDecision{Allowed: true, MonthlyLimit: monthlyLimit, MonthlyRemaining: monthlyLimit, Reason: "redis_unavailable"}
		}
		if ok {
			monthlyUsed = used
		}
	}

	if fiveHourLimit > 0 && int(fiveHourUsed) >= fiveHourLimit {
		return TokenDecision{
			Allowed:           false,
			FiveHourLimit:     fiveHourLimit,
			FiveHourRemaining: 0,
			Reason:            "five_hour_token_limit_exceeded",
		}
	}
	if monthlyLimit > 0 && int(monthlyUsed) >= monthlyLimit {
		return TokenDecision{
			Allowed:          false,
			MonthlyLimit:     monthlyLimit,
			MonthlyRemaining: 0,
			Reason:           "monthly_token_limit_exceeded",
		}
	}
	if dailyLimit > 0 && int(dailyUsed) >= dailyLimit {
		return TokenDecision{
			Allowed:   false,
			Limit:     dailyLimit,
			Remaining: 0,
			Reason:    "daily_token_limit_exceeded",
		}
	}

	remaining := 0
	if dailyLimit > 0 {
		remaining = dailyLimit - int(dailyUsed)
		if remaining < 0 {
			remaining = 0
		}
	}
	fiveHourRemaining := 0
	if fiveHourLimit > 0 {
		fiveHourRemaining = fiveHourLimit - int(fiveHourUsed)
		if fiveHourRemaining < 0 {
			fiveHourRemaining = 0
		}
	}
	monthlyRemaining := 0
	if monthlyLimit > 0 {
		monthlyRemaining = monthlyLimit - int(monthlyUsed)
		if monthlyRemaining < 0 {
			monthlyRemaining = 0
		}
	}
	return TokenDecision{
		Allowed:           true,
		Limit:             dailyLimit,
		Remaining:         remaining,
		FiveHourLimit:     fiveHourLimit,
		FiveHourRemaining: fiveHourRemaining,
		MonthlyLimit:      monthlyLimit,
		MonthlyRemaining:  monthlyRemaining,
	}
}

func (l *Limiter) addTokenUsageRedis(key string, tokens int) {
	dailyLimit := l.resolveDailyTokenLimit(key)
	fiveHourLimit := l.resolveFiveHourTokenLimit(key)
	monthlyLimit := l.resolveMonthlyTokenLimit(key)
	if (dailyLimit <= 0 && fiveHourLimit <= 0 && monthlyLimit <= 0) || tokens <= 0 {
		return
	}

	now := l.nowUTC()
	day := now.Format("20060102")
	if dailyLimit > 0 {
		redisKey := fmt.Sprintf("%s:tok:%s:%s", l.redisPrefix, key, day)
		if _, err := l.redisClient.IncrBy(context.Background(), redisKey, int64(tokens)); err == nil {
			_ = l.redisClient.Expire(context.Background(), redisKey, 48*time.Hour)
		}
	}
	if fiveHourLimit > 0 {
		fiveHourWindow := l.fiveHourBucket(now)
		redisKey := fmt.Sprintf("%s:tok5h:%s:%s", l.redisPrefix, key, fiveHourWindow)
		if _, err := l.redisClient.IncrBy(context.Background(), redisKey, int64(tokens)); err == nil {
			_ = l.redisClient.Expire(context.Background(), redisKey, 8*time.Hour)
		}
	}
	if monthlyLimit > 0 {
		month := now.Format("200601")
		redisKey := fmt.Sprintf("%s:tokm:%s:%s", l.redisPrefix, key, month)
		if _, err := l.redisClient.IncrBy(context.Background(), redisKey, int64(tokens)); err == nil {
			_ = l.redisClient.Expire(context.Background(), redisKey, 40*24*time.Hour)
		}
	}
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

func (l *Limiter) resolveFiveHourTokenLimit(key string) int {
	if l == nil {
		return 0
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	if policy, ok := l.policies[key]; ok {
		return policy.FiveHourTokenLimit
	}
	return 0
}

func (l *Limiter) resolveMonthlyTokenLimit(key string) int {
	if l == nil {
		return 0
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	if policy, ok := l.policies[key]; ok {
		return policy.MonthlyTokenLimit
	}
	return 0
}

func (l *Limiter) fiveHourBucket(now time.Time) string {
	utc := now.UTC()
	block := (utc.Hour() / 5) * 5
	return fmt.Sprintf("%04d%02d%02d%02d", utc.Year(), int(utc.Month()), utc.Day(), block)
}

func (l *Limiter) nowUTC() time.Time {
	fn := l.nowFn
	if fn == nil {
		fn = time.Now
	}
	return fn().UTC()
}
