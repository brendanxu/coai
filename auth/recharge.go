package auth

import (
	"chat/globals"
	"chat/plans"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// QuotaConfig is the parsed shape of gtk_plan.quota_config JSON.
// Currently only quota is consumed by recharge; models is reserved for a
// future chat allowlist.
type QuotaConfig struct {
	Quota  float32  `json:"quota"`
	Models []string `json:"models,omitempty"`
}

// RedeemPlanForOrder is the provider-agnostic L2 token plan redeem entrypoint.
//
// Both LemonSqueezy and Hupijiao call this after their event is verified and
// classified as custom_data.type == "plan". The gtk_user_plan insert and quota
// increment happen in one SQL transaction, so a failed quota write rolls back
// the plan binding as well.
//
// Idempotency is anchored by gtk_user_plan.order_id. If the same provider order
// is seen twice, the second call is a no-op.
func RedeemPlanForOrder(db *sql.DB, userID int64, planCode string, orderID string) error {
	if db == nil {
		return errors.New("auth: RedeemPlanForOrder requires db")
	}
	if userID == 0 || planCode == "" || orderID == "" {
		return errors.New("auth: RedeemPlanForOrder requires userID + planCode + orderID")
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("redeem: begin tx: %w", err)
	}
	defer tx.Rollback()

	var existing int64
	if err := tx.QueryRow(`SELECT id FROM gtk_user_plan WHERE order_id = ?`, orderID).Scan(&existing); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("redeem: check existing: %w", err)
		}
	} else {
		globals.Info(fmt.Sprintf("auth: order %s already redeemed (gtk_user_plan id=%d); idempotent no-op", orderID, existing))
		return nil
	}

	var plan plans.Plan
	err = tx.QueryRow(`
		SELECT id, code, name, type, price_cents, duration_days, quota_config, is_active
		FROM gtk_plan WHERE code = ? AND is_active = TRUE
	`, planCode).Scan(
		&plan.ID,
		&plan.Code,
		&plan.Name,
		&plan.Type,
		&plan.PriceCents,
		&plan.DurationDays,
		&plan.QuotaConfig,
		&plan.IsActive,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("redeem: plan code=%q not found or inactive", planCode)
		}
		return fmt.Errorf("redeem: read plan: %w", err)
	}

	var cfg QuotaConfig
	if plan.QuotaConfig.Valid && plan.QuotaConfig.String != "" {
		if err := json.Unmarshal([]byte(plan.QuotaConfig.String), &cfg); err != nil {
			return fmt.Errorf("redeem: parse quota_config %q: %w", plan.QuotaConfig.String, err)
		}
	}
	if cfg.Quota <= 0 {
		return fmt.Errorf("redeem: plan %q has non-positive quota %f", planCode, cfg.Quota)
	}

	expireAt := time.Now().AddDate(0, 0, int(plan.DurationDays))
	_, err = tx.Exec(`
		INSERT INTO gtk_user_plan (user_id, plan_id, status, expire_at, order_id, purchased_at)
		VALUES (?, ?, 'active', ?, ?, CURRENT_TIMESTAMP)
	`, userID, plan.ID, expireAt, orderID)
	if err != nil {
		if isRechargeDuplicateErr(err) {
			globals.Info(fmt.Sprintf("auth: order %s already redeemed; idempotent no-op", orderID))
			return nil
		}
		return fmt.Errorf("redeem: insert gtk_user_plan: %w", err)
	}

	quotaUpsertSQL := `
		INSERT INTO quota (user_id, quota, used) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE quota = quota + ?
	`
	if globals.SqliteEngine {
		quotaUpsertSQL = `
			INSERT INTO quota (user_id, quota, used) VALUES (?, ?, ?) ON CONFLICT(user_id) DO UPDATE SET quota = quota + ?
		`
	}
	_, err = tx.Exec(quotaUpsertSQL, userID, cfg.Quota, 0., cfg.Quota)
	if err != nil {
		return fmt.Errorf("redeem: increase quota: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("redeem: commit: %w", err)
	}

	globals.Info(fmt.Sprintf("auth: redeemed plan=%s for user=%d order=%s quota+=%f",
		planCode, userID, orderID, cfg.Quota))
	return nil
}

func isRechargeDuplicateErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Error 1062") ||
		strings.Contains(msg, "UNIQUE constraint") ||
		strings.Contains(msg, "PRIMARY KEY")
}
