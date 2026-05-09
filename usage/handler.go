// HTTP handler for the menu bar app's usage feed.
//
//   GET /api/v1/usage/me
//
// Authentication
//   Bearer sk-xxx is parsed by middleware.AuthMiddleware (registered globally
//   in middleware.RegisterMiddleware). The middleware sets c.Get("auth"),
//   c.Get("user") (username), c.Get("agent") ("api" for sk-xxx, "token" for
//   session). We DON'T re-parse the header here — we trust the middleware
//   set the auth flag — but we DO emit a real 401 status if the flag is
//   false (auth.RequireAuth would return 200 + status:false body, which the
//   menu bar app's auto-clear-token logic in Agent 4 doesn't recognize).
//
// Rate limit
//   1 req per 10s per token. Uses Redis (utils.IncrWithLimit) when the
//   global cache is connected. Falls back to a process-local sync.Map when
//   the cache is nil — fine for Stage 1 alpha (single-process), tightened
//   in Wave 2 if multi-replica.
//
// Query params
//   since   ISO date or "today" (default "today"). Reserved for the
//           "show me last 7 days" use case once schema gains daily rollups;
//           today the only honored value is "today" (= today midnight SGT).
//   kind    summary | recent_calls | hourly | all (default summary)
//
// Response
//   200 with the spec-shaped JSON. _estimated:true is set whenever the
//   schema-gap fields (cache_read, cache_savings, tokens_in/out split) are
//   imputed.
package usage

import (
	"chat/auth"
	"chat/connection"
	"chat/globals"
	"chat/utils"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

// rate-limit knobs. Window = 10s, count = 1 → "1 req per 10s per token".
const (
	rateLimitWindowSec = 10
	rateLimitCount     = 1
)

// in-memory fallback rate-limiter state (keyed by user ID stringified —
// not by raw token, so we never pin token bytes in process memory). Used
// when redis is nil. sync.Map is the right primitive: rare contention,
// per-key writes, no need for full-table locking.
var memLimiter sync.Map // key: string (user-id) → val: time.Time (window start)

// Register wires GET /api/v1/usage/me onto the main API router group.
// Called from main.go:registerApiRouter alongside payment.Register etc.
func Register(app *gin.RouterGroup) {
	app.GET("/v1/usage/me", UsageMeAPI)
}

// UsageMeAPI is the single handler. See package doc for spec.
func UsageMeAPI(c *gin.Context) {
	// 1. Auth gate. Middleware already parsed the header; we just check
	//    whether it found a valid user. We emit a real 401 (not 200 +
	//    status:false) so the menu bar can drop the keyring entry on
	//    revoked tokens and reopen its setup window.
	if !c.GetBool("auth") {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "missing or invalid Bearer token",
		})
		return
	}
	user := auth.GetUser(c)
	if user == nil {
		// Defensive: c.GetBool("auth") was true but GetUser returned nil.
		// Shouldn't happen given the middleware contract, but bail with
		// 401 rather than panic.
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "auth context missing user",
		})
		return
	}

	db := utils.GetDBFromContext(c)
	userID := user.GetID(db)
	if userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "user resolution failed",
		})
		return
	}

	// 2. Rate limit. 1 req per 10s per user.
	if retryAfter, blocked := checkRateLimit(c, userID); blocked {
		c.Header("Retry-After", strconv.Itoa(retryAfter))
		c.JSON(http.StatusTooManyRequests, gin.H{
			"success": false,
			"message": "rate limit: 1 req per 10s",
		})
		return
	}

	// 3. Parse params. since is reserved (today-only for v1); kind drives
	//    which top-level fields populate.
	kind := c.DefaultQuery("kind", "summary")
	switch kind {
	case "summary", "recent_calls", "hourly", "all":
		// ok
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "kind must be one of: summary, recent_calls, hourly, all",
		})
		return
	}

	// 4. Compute boundaries (SGT today midnight + month-1).
	now := time.Now()
	todayStart, monthStart := TodayBoundsSGT(now)

	agg := NewAggregator(db)

	// 5. Build response. Always populate "currency" + "_estimated" so the
	//    client can render the partial-data badge regardless of kind.
	resp := gin.H{
		"currency":   Currency,
		"_estimated": true, // schema-gap: cache_*, tokens split, model. See aggregator.go header.
	}

	// summary
	if kind == "summary" || kind == "all" {
		s, err := agg.SummaryFor(userID, todayStart, monthStart, now.In(SGTOffset))
		if err != nil {
			globals.Warn("usage: summary failed: " + err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "summary aggregate failed",
			})
			return
		}
		resp["summary"] = s
	}

	// recent_calls
	if kind == "recent_calls" || kind == "all" {
		calls, err := agg.RecentCallsFor(userID, 10)
		if err != nil {
			globals.Warn("usage: recent calls failed: " + err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "recent_calls aggregate failed",
			})
			return
		}
		resp["recent_calls"] = calls
	}

	// hourly
	if kind == "hourly" || kind == "all" {
		hourly, err := agg.HourlyFor(userID, todayStart)
		if err != nil {
			globals.Warn("usage: hourly failed: " + err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "hourly aggregate failed",
			})
			return
		}
		resp["hourly"] = hourly
	}

	c.JSON(http.StatusOK, resp)
}

// checkRateLimit returns (retryAfterSeconds, blocked). Prefers Redis when
// available, falls back to a process-local sync.Map otherwise. Either way
// the per-user budget is 1 request per rateLimitWindowSec seconds.
//
// Redis path uses utils.IncrWithLimit (the same primitive throttle.go +
// waitlist use). Fail-open semantics on a Redis error — same call as the
// rest of the codebase.
func checkRateLimit(c *gin.Context, userID int64) (int, bool) {
	key := "rate:usage:" + strconv.FormatInt(userID, 10)

	// Try Redis first via the typed-context helper. If the cache slot is
	// nil-typed (test path) or missing, we fall through to in-memory.
	cache := tryGetCache(c)
	if cache != nil {
		allowed, err := utils.IncrWithLimit(cache, key, 1, rateLimitCount, rateLimitWindowSec)
		if err != nil {
			globals.Warn("usage: redis rate-limit check failed (fail-open): " + err.Error())
			return 0, false
		}
		if !allowed {
			return rateLimitWindowSec, true
		}
		return 0, false
	}

	// In-memory fallback.
	now := time.Now()
	if v, ok := memLimiter.Load(key); ok {
		windowStart := v.(time.Time)
		elapsed := now.Sub(windowStart).Seconds()
		if elapsed < float64(rateLimitWindowSec) {
			retry := rateLimitWindowSec - int(elapsed)
			if retry < 1 {
				retry = 1
			}
			return retry, true
		}
	}
	memLimiter.Store(key, now)
	return 0, false
}

// tryGetCache returns the redis client from gin context, or nil if either
// (a) it's not registered, or (b) it's the typed-nil that tests inject.
// Avoids the panic in utils.GetCacheFromContext when the slot is missing.
func tryGetCache(c *gin.Context) *redis.Client {
	v, ok := c.Get("cache")
	if !ok {
		// fall back to the package-global; production main.go connects this.
		return connection.Cache
	}
	cache, ok := v.(*redis.Client)
	if !ok || cache == nil {
		return nil
	}
	return cache
}

// ResetMemLimiterForTest clears the in-memory rate-limiter. Test-only;
// exported with the For-Test suffix so callers know it's not for production.
func ResetMemLimiterForTest() {
	memLimiter.Range(func(k, _ interface{}) bool {
		memLimiter.Delete(k)
		return true
	})
}
