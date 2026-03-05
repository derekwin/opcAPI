package quota

import (
	"testing"
	"time"
)

func TestAllowRequest(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	l := NewLimiter(2, 0)
	l.nowFn = func() time.Time { return base }

	d1 := l.AllowRequest("k1")
	d2 := l.AllowRequest("k1")
	d3 := l.AllowRequest("k1")

	if !d1.Allowed || !d2.Allowed {
		t.Fatalf("first two requests should be allowed")
	}
	if d3.Allowed {
		t.Fatalf("third request should be denied")
	}
	if d3.RetryAfterSec <= 0 {
		t.Fatalf("expected positive retry-after")
	}
}

func TestAllowRequestResetsOnNextMinute(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	current := base
	l := NewLimiter(1, 0)
	l.nowFn = func() time.Time { return current }

	if !l.AllowRequest("k1").Allowed {
		t.Fatalf("first request should be allowed")
	}
	if l.AllowRequest("k1").Allowed {
		t.Fatalf("second request in same minute should be denied")
	}

	current = base.Add(61 * time.Second)
	if !l.AllowRequest("k1").Allowed {
		t.Fatalf("request in next minute should be allowed")
	}
}

func TestDailyTokenLimit(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	current := base
	l := NewLimiter(0, 10)
	l.nowFn = func() time.Time { return current }

	if !l.AllowTokenUsage("k1").Allowed {
		t.Fatalf("initial token usage should be allowed")
	}
	l.AddTokenUsage("k1", 10)
	if l.AllowTokenUsage("k1").Allowed {
		t.Fatalf("token usage should be denied after hitting limit")
	}

	current = base.Add(24 * time.Hour)
	if !l.AllowTokenUsage("k1").Allowed {
		t.Fatalf("token usage should reset next day")
	}
}

func TestPerKeyPolicyOverridesDefault(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	l := NewLimiter(10, 100)
	l.nowFn = func() time.Time { return base }
	l.SetKeyPolicies(map[string]KeyPolicy{
		"k-special": {RPMLimit: 1, DailyTokenLimit: 5},
	})

	if !l.AllowRequest("k-special").Allowed {
		t.Fatalf("first request should be allowed")
	}
	if l.AllowRequest("k-special").Allowed {
		t.Fatalf("second request should be denied by per-key rpm")
	}

	if !l.AllowTokenUsage("k-special").Allowed {
		t.Fatalf("initial token usage should be allowed")
	}
	l.AddTokenUsage("k-special", 5)
	if l.AllowTokenUsage("k-special").Allowed {
		t.Fatalf("token usage should be denied by per-key daily limit")
	}
}

func TestFiveHourAndMonthlyTokenLimits(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	current := base
	l := NewLimiter(0, 0)
	l.nowFn = func() time.Time { return current }
	l.SetKeyPolicies(map[string]KeyPolicy{
		"k-member": {FiveHourTokenLimit: 20, MonthlyTokenLimit: 30},
	})

	if !l.AllowTokenUsage("k-member").Allowed {
		t.Fatalf("initial token usage should be allowed")
	}
	l.AddTokenUsage("k-member", 20)
	d := l.AllowTokenUsage("k-member")
	if d.Allowed || d.Reason != "five_hour_token_limit_exceeded" {
		t.Fatalf("expected five-hour limit exceeded, got %+v", d)
	}

	current = base.Add(5*time.Hour + time.Minute)
	if !l.AllowTokenUsage("k-member").Allowed {
		t.Fatalf("five-hour window should reset")
	}
	l.AddTokenUsage("k-member", 11)
	d = l.AllowTokenUsage("k-member")
	if d.Allowed || d.Reason != "monthly_token_limit_exceeded" {
		t.Fatalf("expected monthly limit exceeded, got %+v", d)
	}

	current = time.Date(2026, 4, 1, 1, 0, 0, 0, time.UTC)
	if !l.AllowTokenUsage("k-member").Allowed {
		t.Fatalf("monthly window should reset")
	}
}
