// HTTP route for capturing demo / contact requests from the marketing
// site. Public — unauthenticated, since the whole point is to let
// non-customers leave their contact info.
//
// Anti-abuse: rate-limited (per-IP), max payload size, basic field
// validation. We do NOT do full anti-bot (turnstile/captcha) yet —
// expected volume is <50/day and founder reviews each lead. Add captcha
// when (a) bot traffic shows up in gtk_lead with status='spam', or
// (b) we automate follow-up.

package lead

import (
	"chat/connection"
	"chat/globals"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Register wires POST /api/gtk/v1/contact onto the gin router.
func Register(app *gin.RouterGroup) {
	app.POST("/gtk/v1/contact", ContactAPI)
}

// ContactRequest is the JSON body. Either wechat OR phone is required;
// at least one channel must be reachable. Other fields are optional
// but recommended for follow-up quality.
type ContactRequest struct {
	Wechat       string `json:"wechat,omitempty"`
	Phone        string `json:"phone,omitempty"`
	HomestayName string `json:"homestay_name,omitempty"`
	HomestayLoc  string `json:"homestay_loc,omitempty"`
	Notes        string `json:"notes,omitempty"`
	Source       string `json:"source,omitempty"` // home/pricing/footer/services etc.
}

// Validation: phone is China mainland 11-digit (1[3-9]\d{9}), wechat is
// 6-20 chars alphanumeric+underscore+hyphen (per WeChat ID rules).
// Notes capped at 500 chars; longer means abuse / paste error.
var (
	phoneRe  = regexp.MustCompile(`^1[3-9]\d{9}$`)
	wechatRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{5,19}$`)
)

const (
	maxNotesLen   = 500
	maxFieldLen   = 128
	rateLimitWindow = 60 * time.Second
	maxLeadsPerWindow = 5
)

func ContactAPI(c *gin.Context) {
	var req ContactRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request: " + err.Error(),
		})
		return
	}

	// Trim + validate.
	req.Wechat = strings.TrimSpace(req.Wechat)
	req.Phone = strings.TrimSpace(req.Phone)
	req.HomestayName = strings.TrimSpace(req.HomestayName)
	req.HomestayLoc = strings.TrimSpace(req.HomestayLoc)
	req.Notes = strings.TrimSpace(req.Notes)
	req.Source = strings.TrimSpace(req.Source)
	if req.Source == "" {
		req.Source = "home"
	}

	if req.Wechat == "" && req.Phone == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请至少填写一个联系方式 (微信号或手机号)",
		})
		return
	}
	if req.Phone != "" && !phoneRe.MatchString(req.Phone) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "手机号格式不正确 (应为 11 位国内手机号)",
		})
		return
	}
	if req.Wechat != "" && !wechatRe.MatchString(req.Wechat) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "微信号格式不正确 (字母开头，6-20 位字母数字下划线)",
		})
		return
	}
	if len(req.Notes) > maxNotesLen {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "备注过长 (最多 500 字)",
		})
		return
	}
	if len(req.HomestayName) > maxFieldLen || len(req.HomestayLoc) > maxFieldLen {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "民宿名/地址过长",
		})
		return
	}

	// Per-IP rate limit: max 5 submissions per minute. Defends against
	// accidental double-submits + low-effort spam.
	if hitRateLimit(c.ClientIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"success": false,
			"message": "请稍后再试 (1 分钟内最多提交 5 次)",
		})
		return
	}

	// Idempotency on (wechat OR phone): re-submit updates the latest row
	// with the same contact channel rather than creating a duplicate.
	id, isNew, err := upsertLead(connection.DB, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "提交失败，请稍后再试: " + err.Error(),
		})
		return
	}

	globals.Info("[lead] new lead captured: id=" + idStr(id) + " source=" + req.Source +
		" wechat=" + redact(req.Wechat) + " phone=" + redact(req.Phone) +
		" new=" + boolStr(isNew))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"lead_id":     id,
			"is_new":      isNew,
			"message":     "我们将在 24 小时内通过你留的联系方式与你沟通",
		},
	})
}

// upsertLead implements idempotency: if a lead with same wechat OR same
// phone exists, update notes/homestay/source; else insert new.
func upsertLead(db *sql.DB, req *ContactRequest) (int64, bool, error) {
	// Look up by phone first (more reliable than wechat ID), then wechat.
	var existingID int64
	if req.Phone != "" {
		row := globals.QueryRowDb(db, `SELECT id FROM gtk_lead WHERE phone = ? LIMIT 1`, req.Phone)
		if err := row.Scan(&existingID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, false, err
		}
	}
	if existingID == 0 && req.Wechat != "" {
		row := globals.QueryRowDb(db, `SELECT id FROM gtk_lead WHERE wechat = ? LIMIT 1`, req.Wechat)
		if err := row.Scan(&existingID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, false, err
		}
	}

	if existingID > 0 {
		// Update existing row. status stays 'new' if current is 'new';
		// don't downgrade contacted → new (founder may have reached out
		// while customer re-submitted).
		_, err := globals.ExecDb(db, `
			UPDATE gtk_lead
			SET wechat = COALESCE(NULLIF(?, ''), wechat),
			    phone = COALESCE(NULLIF(?, ''), phone),
			    homestay_name = COALESCE(NULLIF(?, ''), homestay_name),
			    homestay_loc = COALESCE(NULLIF(?, ''), homestay_loc),
			    notes = COALESCE(NULLIF(?, ''), notes),
			    source = ?
			WHERE id = ?
		`, req.Wechat, req.Phone, req.HomestayName, req.HomestayLoc, req.Notes, req.Source, existingID)
		if err != nil {
			return 0, false, err
		}
		return existingID, false, nil
	}

	res, err := globals.ExecDb(db, `
		INSERT INTO gtk_lead
			(wechat, phone, homestay_name, homestay_loc, notes, source, status)
		VALUES (?, ?, ?, ?, ?, ?, 'new')
	`, req.Wechat, req.Phone, req.HomestayName, req.HomestayLoc, req.Notes, req.Source)
	if err != nil {
		return 0, false, err
	}
	id, _ := res.LastInsertId()
	return id, true, nil
}

// rate limiter — simple in-memory map keyed by IP. Enough for v0.10
// expected volume (<50 leads/day). Memory bounded by GC of expired
// entries on each call. Replace with redis if traffic grows.
var (
	rlMu  sync.Mutex
	rlMap = map[string][]time.Time{}
)

func hitRateLimit(ip string) bool {
	if ip == "" {
		return false // can't track, let it through
	}
	rlMu.Lock()
	defer rlMu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rateLimitWindow)

	// Prune old entries for this IP.
	hits := rlMap[ip]
	pruned := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}

	if len(pruned) >= maxLeadsPerWindow {
		rlMap[ip] = pruned
		return true
	}

	rlMap[ip] = append(pruned, now)

	// Periodic GC: every 100 hits across all IPs, prune empty entries.
	rlGC++
	if rlGC%100 == 0 {
		for k, v := range rlMap {
			if len(v) == 0 {
				delete(rlMap, k)
			}
		}
	}
	return false
}

var rlGC int

// Helpers — kept tiny + private.

func redact(s string) string {
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
}

func idStr(id int64) string {
	return strconv.FormatInt(id, 10)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
