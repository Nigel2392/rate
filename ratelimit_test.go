package rate_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nigel2392/cache"
	"github.com/Nigel2392/rate"
)

func TestGenericLimit_CheckAndReset(t *testing.T) {
	limitCache := cache.NewMemoryCache(5 * time.Minute)
	limit := rate.Limit[rate.ACL[testStringKey], rate.ACL[testStringKey], testStringKey]{
		Domain:      []string{"api", "v1"},
		MaxAttempts: 2,
		Cache:       limitCache, // Use real default cache backend
		Period:      5 * time.Minute,
	}

	ctx := context.Background()
	user := testStringKey("user_check_1")

	// Ensure clean slate before test
	_ = limit.Reset(ctx, user)

	// Attempt 1
	err := limit.Check(ctx, user)
	if err != nil {
		t.Fatalf("expected nil on attempt 1, got: %v", err)
	}

	// Attempt 2
	err = limit.Check(ctx, user)
	if err != nil {
		t.Fatalf("expected nil on attempt 2, got: %v", err)
	}

	// Attempt 3 - Should fail with RateLimitExceeded
	err = limit.Check(ctx, user)
	if !errors.Is(err, rate.ErrRateLimit) {
		t.Fatalf("expected ErrRateLimit on attempt 3, got: %v", err)
	}

	// Reset limits
	err = limit.Reset(ctx, user)
	if err != nil {
		t.Fatalf("failed to reset rate limit: %v", err)
	}

	// Attempt 4 (post-reset) - Should pass
	k, err := limit.GetKey(user)
	if err != nil {
		t.Fatalf("error when generating key for %v: %v", user, err)
	}

	t.Logf("generated key: %q", k)

	n, err := limitCache.CounterValue(ctx, k)
	if n > 0 || !errors.Is(err, cache.ErrItemNotFound) {
		t.Fatal("unexpected cache item found after reset")
	}
}

func TestGenericLimit_WhitelistBlacklist(t *testing.T) {
	whitelist := rate.NewListACL[testStringKey]("admin_user")
	blacklist := rate.NewListACL[testStringKey]("banned_user")

	limitCache := cache.NewMemoryCache(5 * time.Minute)
	limit := rate.Limit[*rate.ListBasedACL[testStringKey], *rate.ListBasedACL[testStringKey], testStringKey]{
		Domain:      []string{"auth"},
		MaxAttempts: 1,
		Cache:       limitCache,
		Whitelist:   whitelist,
		Blacklist:   blacklist,
	}

	ctx := context.Background()

	// Test Blacklist - Immediate block
	err := limit.Check(ctx, "banned_user")
	if !errors.Is(err, rate.ErrBlacklist) && !errors.Is(err, rate.ErrRateLimit) {
		t.Fatalf("expected ErrBlacklist, got: %v", err)
	}

	k, err := limit.GetKey("admin_user")
	if err != nil {
		t.Fatal("failed to generate limit cache key for admin user")
	}

	t.Logf("generated key: %q", k)

	// Test Whitelist - Bypass rate limits completely
	for i := 0; i < 100; i++ {
		time.Sleep(5 * time.Millisecond)
		err = limit.Check(ctx, "admin_user")
		if err != nil {
			t.Fatalf("expected whitelisted user to bypass limit on attempt %d, got: %v", i+1, err)
		}
	}

	n, err := limitCache.CounterValue(ctx, k)
	if n > 0 || !errors.Is(err, cache.ErrItemNotFound) {
		t.Fatal("unexpected cache item found after reset")
	}
}

func TestGenericLimit_SubdomainLogic(t *testing.T) {
	rootLimit := rate.Limit[rate.ACL[testStringKey], rate.ACL[testStringKey], testStringKey]{
		Domain:      []string{"root"},
		MaxAttempts: 2,
		Cache:       cache.NewMemoryCache(5 * time.Minute),
	}

	subLimit := rootLimit.Subdomain(context.Background(), "sub1", "sub2")

	ctx := context.Background()
	user := testStringKey("user_sub_test")

	_ = rootLimit.Reset(ctx, user)
	_ = subLimit.Reset(ctx, user)

	// Use up the subdomain limits
	_ = subLimit.Check(ctx, user)    // Attempt 1
	_ = subLimit.Check(ctx, user)    // Attempt 2
	err := subLimit.Check(ctx, user) // Attempt 3 -> ErrRateLimit
	if !errors.Is(err, rate.ErrRateLimit) {
		t.Fatalf("expected subdomain to trigger ErrRateLimit, got: %v", err)
	}

	// Verify the root limit remains completely independent of the subdomain
	err = rootLimit.Check(ctx, user)
	if err != nil {
		t.Fatalf("expected root limit to be unbothered by subdomain hits, got: %v", err)
	}
}

func TestGenericLimit_Concurrency(t *testing.T) {
	limitCache := cache.NewMemoryCache(5 * time.Minute)
	limit := rate.Limit[rate.ACL[testStringKey], rate.ACL[testStringKey], testStringKey]{
		Domain:      []string{"concurrent_api"},
		MaxAttempts: 50,
		Cache:       limitCache,
	}

	ctx := context.Background()
	user := testStringKey("stress_tester")

	var (
		wg        sync.WaitGroup
		successes int32
		failures  int32
		totalReqs = 200
	)

	// Blast the rate limiter with 200 concurrent requests
	for i := 0; i < totalReqs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := limit.Check(ctx, user)
			if err == nil {
				atomic.AddInt32(&successes, 1)
			} else if errors.Is(err, rate.ErrRateLimit) {
				atomic.AddInt32(&failures, 1)
			}
		}()
	}

	wg.Wait()

	if successes != 50 {
		t.Errorf("expected exactly 50 successful requests, got %d", successes)
	}
	if failures != 150 {
		t.Errorf("expected exactly 150 failed requests, got %d", failures)
	}
}

func TestGenericLimit_BanDurationExpiration(t *testing.T) {
	limitCache := cache.NewMemoryCache(5 * time.Minute)

	// Extremely short ban duration for testing
	banTime := 50 * time.Millisecond

	limit := rate.Limit[rate.ACL[testStringKey], rate.ACL[testStringKey], testStringKey]{
		Domain:      []string{"timeout_api"},
		MaxAttempts: 1,
		BanDuration: banTime,
		Cache:       limitCache,
	}

	ctx := context.Background()
	user := testStringKey("timeout_user")

	// 1st request -> Passes
	if err := limit.Check(ctx, user); err != nil {
		t.Fatalf("expected 1st attempt to pass: %v", err)
	}

	// 2nd request -> Hits limit and drops ban hammer
	err := limit.Check(ctx, user)
	if !errors.Is(err, rate.ErrRateLimit) {
		t.Fatalf("expected 2nd attempt to trigger ban: %v", err)
	}

	// Sleep slightly longer than the ban duration
	time.Sleep(banTime + (20 * time.Millisecond))

	// 3rd request -> Should pass because ban expired
	if err := limit.Check(ctx, user); err != nil {
		t.Fatalf("expected attempt to pass after ban expiration, got: %v", err)
	}
}

func TestGenericLimit_SubdomainIsolation(t *testing.T) {
	limitCache := cache.NewMemoryCache(5 * time.Minute)
	rootLimit := rate.Limit[rate.ACL[testStringKey], rate.ACL[testStringKey], testStringKey]{
		Domain:      []string{"system"},
		MaxAttempts: 3,
		Cache:       limitCache,
	}

	loginLimit := rootLimit.Subdomain(context.Background(), "auth", "login")
	resetLimit := rootLimit.Subdomain(context.Background(), "auth", "reset_password")

	ctx := context.Background()
	user := testStringKey("isolated_user")

	// Exhaust the login subdomain limit
	_ = loginLimit.Check(ctx, user)
	_ = loginLimit.Check(ctx, user)
	_ = loginLimit.Check(ctx, user)

	err := loginLimit.Check(ctx, user)
	if !errors.Is(err, rate.ErrRateLimit) {
		t.Fatal("expected login limit to be exhausted")
	}

	// Verify root and sibling limits are completely unaffected
	if err := resetLimit.Check(ctx, user); err != nil {
		t.Fatalf("sibling subdomain should be unaffected, got: %v", err)
	}

	if err := rootLimit.Check(ctx, user); err != nil {
		t.Fatalf("root domain should be unaffected by subdomain limits, got: %v", err)
	}
}

func TestGenericLimit_CustomKeyGen(t *testing.T) {
	limitCache := cache.NewMemoryCache(5 * time.Minute)
	limit := rate.Limit[rate.ACL[testStringKey], rate.ACL[testStringKey], testStringKey]{
		Domain:      []string{"custom"},
		MaxAttempts: 1,
		Cache:       limitCache,
		KeyGen: func(domain []string, data testStringKey) (string, error) {
			return "static_hardcoded_key", nil
		},
	}

	ctx := context.Background()
	_ = limit.Check(ctx, "user_a")
	_ = limit.Check(ctx, "user_b") // This should fail because KeyGen collapses them into the same key

	err := limit.Check(ctx, "user_c")
	if !errors.Is(err, rate.ErrRateLimit) {
		t.Fatal("expected user_c to be blocked due to shared custom key")
	}

	// Verify the cache contains exactly our custom key
	n, err := limitCache.CounterValue(ctx, "static_hardcoded_key")
	if err != nil || n != 3 {
		t.Fatalf("expected custom key to have 3 hits, got %d (err: %v)", n, err)
	}
}

func TestGenericLimit_ErrorHandling(t *testing.T) {
	// Let's create an artificial scenario where the cache fails by passing a canceled context
	limitCache := cache.NewMemoryCache(5 * time.Minute)

	limitContinue := rate.Limit[rate.ACL[testStringKey], rate.ACL[testStringKey], testStringKey]{
		Domain:      []string{"err_continue"},
		MaxAttempts: 1,
		Cache:       limitCache,
		OnError: func(err error) bool {
			return true // Fail open: allow request if cache fails
		},
	}

	limitAbort := rate.Limit[rate.ACL[testStringKey], rate.ACL[testStringKey], testStringKey]{
		Domain:      []string{"err_abort"},
		MaxAttempts: 1,
		Cache:       limitCache,
		OnError: func(err error) bool {
			return false // Fail closed: block request if cache fails
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // instantly cancel so cache operations fail

	// Test Fail Open
	err := limitContinue.Check(ctx, "user_1")
	if err != nil {
		t.Fatalf("expected limit to continue on error, got: %v", err)
	}

	// Test Fail Closed
	err = limitAbort.Check(ctx, "user_2")
	if !errors.Is(err, rate.ErrCache) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected a cache/context error, got: %v", err)
	}
}
