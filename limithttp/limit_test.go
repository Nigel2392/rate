package limithttp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nigel2392/cache"
	"github.com/Nigel2392/rate"
	"github.com/Nigel2392/rate/limithttp"
)

func init() {
	rate.RegisterMatchType[*http.Request](func(r *http.Request) []string {
		return []string{r.RemoteAddr}
	})
}

func TestRatelimitMiddleware_Standard(t *testing.T) {
	limitCfg := &limithttp.Limit[limithttp.HTTPACL, limithttp.HTTPACL]{
		Limit: rate.Limit[limithttp.HTTPACL, limithttp.HTTPACL, *http.Request]{
			Domain:      []string{"http_standard"},
			MaxAttempts: 2,
			Cache:       cache.NewMemoryCache(1 * time.Minute),
		},
	}

	middleware := limithttp.RatelimitMiddleware(limitCfg)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:8080"

	// Req 1 (OK)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	// Req 2 (OK)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	// Req 3 (Blocked by default handleBlockDefault)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got %d", rr.Code)
	}
}

func TestRatelimitMiddleware_CustomHandlers(t *testing.T) {
	limitCfg := &limithttp.Limit[limithttp.HTTPACL, limithttp.HTTPACL]{
		Limit: rate.Limit[limithttp.HTTPACL, limithttp.HTTPACL, *http.Request]{
			Domain:      []string{"http_custom"},
			MaxAttempts: 1,
			Cache:       cache.NewMemoryCache(1 * time.Minute),
		},
		Blocked: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-RateLimit-Exceeded", "true")
			http.Error(w, "Enhance Your Calm", http.StatusTooManyRequests)
		},
	}

	handler := limithttp.RatelimitMiddleware(limitCfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.2:8080"

	// Use up the limit
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// Second request triggers custom block
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr.Code)
	}
	if rr.Header().Get("X-RateLimit-Exceeded") != "true" {
		t.Fatal("custom block handler did not set expected header")
	}
}

func TestRatelimitMiddleware_ContextResetIsolation(t *testing.T) {
	limitCfg := &limithttp.Limit[limithttp.HTTPACL, limithttp.HTTPACL]{
		Limit: rate.Limit[limithttp.HTTPACL, limithttp.HTTPACL, *http.Request]{
			Domain:      []string{"http_reset_isolation"},
			MaxAttempts: 1,
			Cache:       cache.NewMemoryCache(1 * time.Minute),
		},
	}

	middleware := limithttp.RatelimitMiddleware(limitCfg)

	// Route A resets its limits (e.g. successful login)
	routeA := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limithttp.ContextResetRateLimit(r.Context(), true)
		w.WriteHeader(http.StatusOK)
	}))

	// Route B does NOT reset its limits (e.g. failed login)
	routeB := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))

	reqA := httptest.NewRequest("POST", "/login", nil)
	reqA.RemoteAddr = "192.168.1.100:1111" // User A

	reqB := httptest.NewRequest("POST", "/login", nil)
	reqB.RemoteAddr = "192.168.1.200:2222" // User B

	// User A hits Route A (Limit hit -> Reset triggered -> Cleared)
	routeA.ServeHTTP(httptest.NewRecorder(), reqA)

	// User A can hit Route A again immediately
	rrA := httptest.NewRecorder()
	routeA.ServeHTTP(rrA, reqA)
	if rrA.Code != http.StatusOK {
		t.Fatalf("User A should have been reset and allowed, got %d", rrA.Code)
	}

	// User B hits Route B (Limit hit -> NO Reset)
	routeB.ServeHTTP(httptest.NewRecorder(), reqB)

	// User B hits Route B again (Should be blocked)
	rrB := httptest.NewRecorder()
	routeB.ServeHTTP(rrB, reqB)
	if rrB.Code != http.StatusForbidden {
		t.Fatalf("User B should have been blocked by default handler, got %d", rrB.Code)
	}
}

func TestRatelimitMiddleware_MissingConfigPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected middleware initialization to panic with nil config")
		}
	}()

	_ = limithttp.RatelimitMiddleware[limithttp.HTTPACL, limithttp.HTTPACL](nil)
}

func TestRatelimitMiddleware_Concurrency(t *testing.T) {
	limitCfg := &limithttp.Limit[limithttp.HTTPACL, limithttp.HTTPACL]{
		Limit: rate.Limit[limithttp.HTTPACL, limithttp.HTTPACL, *http.Request]{
			Domain:      []string{"http_concurrent"},
			MaxAttempts: 10,
			Cache:       cache.NewMemoryCache(1 * time.Minute),
		},
	}

	handler := limithttp.RatelimitMiddleware(limitCfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.5:8080"

	var wg sync.WaitGroup
	var successes int32

	// Fire 50 concurrent HTTP requests
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code == http.StatusOK {
				atomic.AddInt32(&successes, 1)
			}
		}()
	}

	wg.Wait()

	if successes != 10 {
		t.Errorf("expected exactly 10 HTTP requests to pass, got %d", successes)
	}
}

func TestRatelimitMiddleware_HeaderWhitelistACL(t *testing.T) {
	// Define a custom FuncACL that checks for a specific bypass header.
	var headerWhitelist rate.FuncACL[*http.Request] = func(ctx context.Context, r *http.Request) (bool, error) {
		return r.Header.Get("X-Bypass-Token") == "super-secret", nil
	}

	limitCfg := &limithttp.Limit[limithttp.HTTPACL, limithttp.HTTPACL]{
		Limit: rate.Limit[limithttp.HTTPACL, limithttp.HTTPACL, *http.Request]{
			Domain:      []string{"http_header_whitelist"},
			MaxAttempts: 1,
			Cache:       cache.NewMemoryCache(1 * time.Minute),
			Whitelist:   headerWhitelist, // Apply the custom ACL
		},
	}

	middleware := limithttp.RatelimitMiddleware(limitCfg)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request 1: Normal user, uses up the limit
	reqNormal := httptest.NewRequest("GET", "/", nil)
	reqNormal.RemoteAddr = "10.0.0.99:8080"

	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, reqNormal)
	if rr1.Code != http.StatusOK {
		t.Fatalf("expected 200 on first normal request, got %d", rr1.Code)
	}

	// Request 2: Normal user, should now be blocked
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, reqNormal)
	if rr2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on second normal request, got %d", rr2.Code)
	}

	// Request 3: Whitelisted user, should bypass the limit entirely despite sharing an IP
	reqWhitelisted := httptest.NewRequest("GET", "/", nil)
	reqWhitelisted.RemoteAddr = "10.0.0.99:8080"
	reqWhitelisted.Header.Set("X-Bypass-Token", "super-secret")

	for i := 0; i < 5; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, reqWhitelisted)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 for whitelisted request on attempt %d, got %d", i+1, rr.Code)
		}
	}
}

func TestRatelimitMiddleware_HeaderBlacklistACL(t *testing.T) {
	// Define a custom FuncACL that blocks a specific User-Agent.
	var badBotBlacklist rate.FuncACL[*http.Request] = func(ctx context.Context, r *http.Request) (bool, error) {
		return r.Header.Get("User-Agent") == "BadBot/1.0", nil
	}

	limitCfg := &limithttp.Limit[limithttp.HTTPACL, limithttp.HTTPACL]{
		Limit: rate.Limit[limithttp.HTTPACL, limithttp.HTTPACL, *http.Request]{
			Domain:      []string{"http_header_blacklist"},
			MaxAttempts: 10,
			Cache:       cache.NewMemoryCache(1 * time.Minute),
			Blacklist:   badBotBlacklist, // Apply the custom ACL
		},
	}

	middleware := limithttp.RatelimitMiddleware(limitCfg)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request 1: Malicious bot
	reqBot := httptest.NewRequest("GET", "/", nil)
	reqBot.RemoteAddr = "10.0.1.50:8080"
	reqBot.Header.Set("User-Agent", "BadBot/1.0")

	rrBot := httptest.NewRecorder()
	handler.ServeHTTP(rrBot, reqBot)

	// Should be blocked immediately on the first try, well below MaxAttempts
	if rrBot.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for blacklisted bot, got %d", rrBot.Code)
	}

	// Request 2: Normal user
	reqNormal := httptest.NewRequest("GET", "/", nil)
	reqNormal.RemoteAddr = "10.0.1.51:8080"
	reqNormal.Header.Set("User-Agent", "GoodBrowser/1.0")

	rrNormal := httptest.NewRecorder()
	handler.ServeHTTP(rrNormal, reqNormal)
	if rrNormal.Code != http.StatusOK {
		t.Fatalf("expected 200 for normal user, got %d", rrNormal.Code)
	}
}
