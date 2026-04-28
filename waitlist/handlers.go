package waitlist

import (
	"chat/globals"
	"chat/utils"
	"database/sql"
	"fmt"
	"net/http"
	"net/mail"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

// validServices is the closed set of service slugs the waitlist accepts.
// Adding a new "Coming Soon" service later is a one-line edit here plus a
// frontend ServiceCard. Keeping this server-side guards against a stale
// frontend leaking arbitrary strings into the audit table.
var validServices = map[string]bool{
	"tax-filing":    true,
	"video-editing": true,
	"any":           true, // visitor wants notifications for everything
}

const (
	emailMaxLen    = 254 // RFC 5321 bound, also matches column width
	emailMinLen    = 5   // shortest possible address: a@b.c
	rateLimitCount = 5
	rateLimitWin   = 60 // seconds
)

// joinRequest is the JSON body of POST /api/waitlist.
type joinRequest struct {
	Email   string `json:"email" binding:"required"`
	Service string `json:"service" binding:"required"`
	Source  string `json:"source"`
}

// HandleJoin captures a visitor's interest in a not-yet-launched service.
//
// POST /api/waitlist
// Body: {"email": "...", "service": "tax-filing|video-editing|any", "source": "marketing-landing"}
//
// Response shapes (CoAI convention: status flag + payload):
//
//	200 {"status": true, "message": "已加入等待列表"}            // new row inserted
//	200 {"status": true, "message": "已在等待列表"}              // duplicate, idempotent
//	400 {"status": false, "error": "..."}                      // validation
//	429 {"status": false, "error": "rate limited"}             // 5/min/IP exceeded
//	500 {"status": false, "error": "..."}                      // DB-down etc.
//
// Rate-limit fail-mode is OPEN: a redis outage logs a warning and lets the
// request through. Email capture matters more than spam protection on a
// non-monetary form, and the unique constraint on (email, service) caps the
// blast radius even under attack.
func HandleJoin(c *gin.Context) {
	var body joinRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": false, "error": "invalid request body"})
		return
	}

	email, err := normalizeAndValidateEmail(body.Email)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": false, "error": err.Error()})
		return
	}

	service := strings.TrimSpace(body.Service)
	if !validServices[service] {
		c.JSON(http.StatusBadRequest, gin.H{"status": false, "error": "unknown service"})
		return
	}

	source := strings.TrimSpace(body.Source)
	if len(source) > 64 {
		source = source[:64]
	}

	cache := utils.GetCacheFromContext(c)
	if limited := checkRateLimit(cache, c.ClientIP()); limited {
		c.JSON(http.StatusTooManyRequests, gin.H{"status": false, "error": "rate limited"})
		return
	}

	db := utils.GetDBFromContext(c)
	inserted, err := insertWaitlistRow(db, email, service, source)
	if err != nil {
		globals.Warn("waitlist: insert failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"status": false, "error": "暂时无法处理，请稍后重试"})
		return
	}

	msg := "已加入等待列表"
	if !inserted {
		msg = "已在等待列表"
	}
	c.JSON(http.StatusOK, gin.H{"status": true, "message": msg})
}

// normalizeAndValidateEmail trims, lowercases, and parses the address with
// stdlib net/mail (RFC 5322). Returns the canonical form on success.
func normalizeAndValidateEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if len(email) < emailMinLen || len(email) > emailMaxLen {
		return "", fmt.Errorf("email length must be %d-%d", emailMinLen, emailMaxLen)
	}
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return "", fmt.Errorf("invalid email")
	}
	return strings.ToLower(addr.Address), nil
}

// checkRateLimit returns true if the caller has exceeded the per-IP budget.
// Reuses utils.IncrWithLimit (the same primitive middleware/throttle.go uses)
// so behavior is identical: atomic INCR + EXPIRE on first hit.
//
// IncrWithLimit returns (true=under-limit, false=over-limit); we negate so
// the caller reads as "is this caller blocked?".
//
// Fail-open: a nil cache (redis never connected) or a redis error logs a
// warning and lets the request through. Email capture matters more than spam
// protection on a non-monetary form. Decision captured 2026-04-28 in the
// Marketing window plan review.
func checkRateLimit(cache *redis.Client, ip string) bool {
	if cache == nil {
		globals.Warn("waitlist: redis unavailable, rate-limit fail-open")
		return false
	}
	key := fmt.Sprintf("rate:waitlist:%s", ip)
	allowed, err := utils.IncrWithLimit(cache, key, 1, rateLimitCount, rateLimitWin)
	if err != nil {
		globals.Warn("waitlist: rate-limit check failed (fail-open): " + err.Error())
		return false
	}
	return !allowed
}

// insertWaitlistRow inserts a row, treating a unique-key violation as a
// successful no-op. Returns true if a new row was actually written.
//
// We intentionally do not use INSERT IGNORE / ON CONFLICT here: the dialect
// gap between MySQL and the sqlite test engine makes engine-agnostic code
// simpler when we just catch the duplicate error string.
func insertWaitlistRow(db *sql.DB, email, service, source string) (bool, error) {
	res, err := globals.ExecDb(db,
		`INSERT INTO gtk_waitlist (email, service, source) VALUES (?, ?, ?)`,
		email, service, source)
	if err != nil {
		if isDuplicateKeyErr(err) {
			return false, nil
		}
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// isDuplicateKeyErr matches MySQL ("Error 1062: Duplicate entry") and sqlite
// ("UNIQUE constraint failed") flavors of unique-violation. Substring-match
// is intentional — pulling in the mysql driver just for an error code would
// pin this package to a driver.
func isDuplicateKeyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Duplicate entry") ||
		strings.Contains(msg, "UNIQUE constraint failed")
}
