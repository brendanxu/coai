package payment

import (
	"chat/auth"
	"chat/globals"
	"chat/utils"
	"database/sql"

	"github.com/gin-gonic/gin"
)

// SubscriptionAPI returns the authenticated user's LemonSqueezy subscription
// state, joined with CoAI's authoritative subscription level + expiry.
//
// GET /api/payment/subscription
//
// Response shape:
//
//	{
//	  "status": true,
//	  "data": {
//	    "level":         1,                  // CoAI subscription.level (0 = none)
//	    "expired_at":    "2026-05-27 12:00:00",
//	    "ls_status":     "active",           // LS-side: active / on_trial / past_due / cancelled / unpaid / expired
//	    "cancelled_at":  null,               // set when LS sent subscription_cancelled
//	    "renews_at":     "2026-05-27 12:00:00",
//	    "test_mode":     true
//	  }
//	}
//
// For users who never subscribed, ls_status is empty string and CoAI's
// level is 0. The frontend UpgradeCTA uses this to render: Upgrade /
// active / cancelled-but-active / past_due / on_trial pills.
type subscriptionResponse struct {
	Level       int    `json:"level"`
	ExpiredAt   string `json:"expired_at"`
	LsStatus    string `json:"ls_status"`
	CancelledAt string `json:"cancelled_at"`
	RenewsAt    string `json:"renews_at"`
	TestMode    bool   `json:"test_mode"`
}

func SubscriptionAPI(c *gin.Context) {
	user := auth.GetUserByCtx(c)
	if user == nil {
		return
	}

	db := utils.GetDBFromContext(c)
	userID := user.GetID(db)

	resp := subscriptionResponse{}

	// CoAI's authoritative state — present even when LS never sent anything.
	var expiredAt sql.NullString
	_ = globals.QueryRowDb(db,
		`SELECT level, expired_at FROM subscription WHERE user_id = ?`, userID,
	).Scan(&resp.Level, &expiredAt)
	if expiredAt.Valid {
		resp.ExpiredAt = expiredAt.String
	}

	// LS-side state — empty for users who never paid via LS.
	var (
		lsStatus    sql.NullString
		cancelledAt sql.NullString
		renewsAt    sql.NullString
		testMode    sql.NullBool
	)
	_ = globals.QueryRowDb(db, `
		SELECT status, cancelled_at, renews_at, test_mode
		FROM gtk_ls_subscription
		WHERE user_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, userID).Scan(&lsStatus, &cancelledAt, &renewsAt, &testMode)
	resp.LsStatus = lsStatus.String
	resp.CancelledAt = cancelledAt.String
	resp.RenewsAt = renewsAt.String
	resp.TestMode = testMode.Bool

	c.JSON(200, gin.H{"status": true, "data": resp})
}
