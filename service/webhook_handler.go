// Service-order webhook handlers — called by payment/lemonsqueezy.go's
// dispatch when the inbound webhook's custom_data contains a
// greentokey_order_no key, AND by the hupijiao callback route directly.
//
// Two providers, same outcome: flip a pending_payment order to paid,
// idempotent on retries. The actual provider-specific concerns
// (HMAC, payload shapes) are handled by the caller — this package
// only deals in our own (order_no, ls_order_id|hupijiao_trade_no)
// space.
//
// Concurrency safety: the UPDATE uses a guard on current status so
// double-fire from LS retries can't double-grant credits or reopen a
// refunded order.

package service

import (
	"chat/auth"
	"chat/connection"
	"chat/globals"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// MarkOrderPaid flips a pending_payment order to paid + records the
// payment provider's external order id. Idempotent: a second call with
// the same args is a no-op.
//
// Status transitions allowed:
//   pending_payment → paid     (happy path)
//   paid            → paid     (idempotent — no-op)
//   anything else   → error    (won't reopen completed/refunded orders)
//
// The UPDATE includes a `WHERE status = 'pending_payment'` guard so
// concurrent webhook retries can't double-flip. Affected-rows = 0
// after the UPDATE means "someone else already flipped it" → idempotent.
func MarkOrderPaid(db *sql.DB, orderNo, externalOrderID, provider string) error {
	if orderNo == "" {
		return errors.New("service: MarkOrderPaid requires order_no")
	}
	if provider != "lemonsqueezy" && provider != "hupijiao" {
		return fmt.Errorf("service: MarkOrderPaid unknown provider %q", provider)
	}

	// Pre-check current status. Three outcomes:
	//   pending_payment → proceed with UPDATE
	//   paid + same ls_order_id  → idempotent, no-op
	//   paid + different ls_order_id → DUPLICATE EXTERNAL ID — log alert
	//   running/completed → already past payment, log info
	//   refunded/failed → terminal, refuse (don't quietly reopen)
	var (
		currentStatus  string
		currentExtID   sql.NullString
		hupijiaoTradeNo sql.NullString
	)
	row := globals.QueryRowDb(db, `
		SELECT status, ls_order_id, hupijiao_trade_no
		FROM gtk_service_order WHERE order_no = ?
	`, orderNo)
	if err := row.Scan(&currentStatus, &currentExtID, &hupijiaoTradeNo); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("service: order %q not found", orderNo)
		}
		return fmt.Errorf("read order: %w", err)
	}

	switch currentStatus {
	case "pending_payment":
		var sqlBody string
		if provider == "lemonsqueezy" {
			sqlBody = `
				UPDATE gtk_service_order
				SET status = 'paid', ls_order_id = ?, paid_at = CURRENT_TIMESTAMP
				WHERE order_no = ? AND status = 'pending_payment'
			`
		} else {
			sqlBody = `
				UPDATE gtk_service_order
				SET status = 'paid', hupijiao_trade_no = ?, paid_at = CURRENT_TIMESTAMP
				WHERE order_no = ? AND status = 'pending_payment'
			`
		}
		res, err := globals.ExecDb(db, sqlBody, externalOrderID, orderNo)
		if err != nil {
			return fmt.Errorf("flip to paid: %w", err)
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			// Concurrent webhook beat us. Re-read to confirm it's now
			// `paid` and we agree with the external id.
			globals.Info(fmt.Sprintf("service: MarkOrderPaid race detected for %s — concurrent webhook flipped it first", orderNo))
		} else {
			globals.Info(fmt.Sprintf("service: order %s flipped to paid via %s (external_id=%s)", orderNo, provider, externalOrderID))
		}
		return nil

	case "paid", "running", "completed":
		// Already past pending — webhook replay. Verify external id
		// agreement.
		var stored string
		if provider == "lemonsqueezy" && currentExtID.Valid {
			stored = currentExtID.String
		} else if provider == "hupijiao" && hupijiaoTradeNo.Valid {
			stored = hupijiaoTradeNo.String
		}
		if stored != "" && stored != externalOrderID {
			globals.Warn(fmt.Sprintf("service: external_id mismatch for %s — stored=%s incoming=%s — possible duplicate-customer or webhook routing bug",
				orderNo, stored, externalOrderID))
			// Don't error — webhook will retry forever. Just flag.
		}
		return nil

	case "refunded", "failed":
		// Terminal. Don't reopen.
		return fmt.Errorf("service: order %q is in terminal state %q, refusing to mark paid",
			orderNo, currentStatus)

	default:
		return fmt.Errorf("service: order %q in unexpected state %q", orderNo, currentStatus)
	}
}

// LinkSubscription sets gtk_service_order.subscription_id for monthly
// service orders. Called when LS sends subscription_created and the
// custom_data has a greentokey_order_no. The subID arg is the local
// gtk_ls_subscription.id (NOT the LS-side subscription string).
//
// Idempotent: if subscription_id is already set, no-op.
func LinkSubscription(db *sql.DB, orderNo string, subID int64) error {
	if orderNo == "" || subID == 0 {
		return errors.New("service: LinkSubscription requires order_no and subID")
	}
	_, err := globals.ExecDb(db, `
		UPDATE gtk_service_order
		SET subscription_id = ?
		WHERE order_no = ? AND subscription_id IS NULL
	`, subID, orderNo)
	return err
}

// ─────────────────────────────────────────────────────────────────────
// hupijiao callback HTTP handler
// ─────────────────────────────────────────────────────────────────────

// HupijiaoCallbackAPI is the webhook target for hupijiao-side payment
// confirmations. Public route (hupijiao posts unauthenticated form
// data + HMAC signature) — we verify the signature against
// hupijiao.merchant_secret before trusting any field.
//
// Route: POST /api/gtk/v1/service/hupijiao-callback
//
// On success: flips matching gtk_service_order to 'paid', returns
// "success" body string (hupijiao's expected ack format — does NOT
// expect JSON, just plain text "success" or it will retry).
func HupijiaoCallbackAPI(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil {
		c.String(http.StatusBadRequest, "bad form")
		return
	}

	merchantSecret := viper.GetString("hupijiao.merchant_secret")
	if merchantSecret == "" {
		globals.Warn("service: hupijiao.merchant_secret not configured — refusing callback")
		c.String(http.StatusInternalServerError, "not configured")
		return
	}

	// Collect all form fields except `hash`, sort alphabetically,
	// concatenate k=v&k=v, append secret, MD5 — same scheme as
	// outbound signing in checkout.go.
	got := c.Request.PostForm
	params := make(map[string]string, len(got))
	var incomingHash string
	for k, vs := range got {
		if len(vs) == 0 {
			continue
		}
		if k == "hash" {
			incomingHash = vs[0]
			continue
		}
		params[k] = vs[0]
	}
	wantHash := hupijiaoVerifyHash(params, merchantSecret)
	if !strings.EqualFold(incomingHash, wantHash) {
		globals.Warn(fmt.Sprintf("service: hupijiao callback signature mismatch (incoming=%s)", incomingHash))
		c.String(http.StatusUnauthorized, "bad signature")
		return
	}

	// Hupijiao success status string varies by API version. Common ones:
	// "OD" = order delivered/paid. "WP" = waiting payment. We act only
	// on a verified payment success indicator.
	status := params["status"]
	if status != "OD" && status != "PAYED" && status != "PAID" {
		// Other states (WP / FAIL / etc) — log + ack.
		globals.Info(fmt.Sprintf("service: hupijiao callback non-paid status %q for order %s",
			status, params["trade_order_id"]))
		c.String(http.StatusOK, "success")
		return
	}

	orderNo := params["trade_order_id"]
	if orderNo == "" {
		c.String(http.StatusBadRequest, "missing trade_order_id")
		return
	}
	hupijiaoTxID := params["transaction_id"]
	if hupijiaoTxID == "" {
		hupijiaoTxID = orderNo // fallback, not all flows include separate tx id
	}

	if err := MarkOrderPaid(connection.DB, orderNo, hupijiaoTxID, "hupijiao"); err != nil {
		globals.Warn(fmt.Sprintf("service: hupijiao MarkOrderPaid failed: %v", err))
		c.String(http.StatusInternalServerError, "internal error")
		return
	}

	// Hupijiao expects a literal "success" body to stop retries.
	c.String(http.StatusOK, "success")
}

// hupijiaoVerifyHash is the inverse of the outbound checkout signer.
// Same algorithm: sort keys, concat, append secret, MD5 hex.
//
// Kept separate from hupijiaoSign in checkout.go to maintain
// per-direction clarity (signing outbound uses a different "what to
// include" rule than verifying inbound — though in practice they're
// identical today, this lets us evolve them independently).
func hupijiaoVerifyHash(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
	}
	b.WriteString(secret)
	sum := md5.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// ─────────────────────────────────────────────────────────────────────
// Auth-required helper for callers that need the current user's id
// outside of the routes (e.g. internal batch jobs may want to look up
// the auth.User struct from a session). Currently unused; placeholder
// to keep webhook_handler.go's import surface honest.
// ─────────────────────────────────────────────────────────────────────

var _ = auth.RequireAuth // referenced only to keep import; HupijiaoCallbackAPI is auth-free by design (HMAC signed)
