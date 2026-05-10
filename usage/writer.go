package usage

import (
	"chat/auth"
	"chat/channel"
	"chat/globals"
	"chat/utils"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const defaultMarkupMultiplier float32 = 1.300

// WriteUsageLog records one row in gtk_app_usage_log per chat call.
//
// Called from manager/chat.go::CollectQuota after user.UseQuota succeeds.
// Synchronous writes keep quota mutation and audit recording in the same
// request path; callers should log errors and continue the chat response path.
func WriteUsageLog(
	db *sql.DB,
	user *auth.User,
	buffer *utils.Buffer,
	serviceLabel string,
) error {
	if db == nil || user == nil || buffer == nil {
		return errors.New("usage: WriteUsageLog requires db + user + buffer")
	}

	chargeInst := resolveCharge(buffer)
	if chargeInst == nil {
		return errors.New("usage: WriteUsageLog requires charge")
	}

	inT, outT, cacheW, cacheR, cacheTTL := tokenBreakdown(buffer)
	clientCharge := clientChargeFor(chargeInst, buffer)
	markup := readMarkupMultiplier(db)
	upstreamCost := clientCharge / markup
	upstreamMicro := int64(upstreamCost * 1_000_000)
	clientMicro := int64(clientCharge * 1_000_000)

	userID := user.GetID(db)
	planID := lookupActivePlanID(db, userID)
	modelID := buffer.Model
	if modelID == "" {
		modelID = fallbackModel(chargeInst)
	}
	if serviceLabel == "" {
		serviceLabel = modelID
	}

	totalTokens := inT + outT + cacheW + cacheR
	costCents := int64(clientCharge * 100)

	_, err := globals.ExecDb(db, `
		INSERT INTO gtk_app_usage_log (
			user_id, plan_id, service, tokens_used, cost_cents,
			model_id, provider, input_tokens, output_tokens,
			cache_write_tokens, cache_read_tokens, cache_ttl,
			upstream_cost_micro, client_charge_micro, markup_multiplier
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		userID, nullableInt64(planID), serviceLabel,
		totalTokens, costCents,
		modelID, providerOf(modelID),
		inT, outT, cacheW, cacheR, cacheTTL,
		upstreamMicro, clientMicro, markup,
	)
	if err != nil {
		return fmt.Errorf("usage: insert log: %w", err)
	}
	return nil
}

func resolveCharge(buffer *utils.Buffer) utils.Charge {
	if buffer == nil {
		return nil
	}
	if buffer.Model != "" && channel.ChargeInstance != nil {
		if charge := channel.ChargeInstance.GetCharge(buffer.Model); charge != nil {
			if charge.IsUnsetType() && buffer.Charge != nil {
				return buffer.Charge
			}
			return charge
		}
	}
	return buffer.Charge
}

func tokenBreakdown(buffer *utils.Buffer) (int64, int64, int64, int64, string) {
	if buffer.Upstream != nil {
		return int64(buffer.Upstream.InputTokens),
			int64(buffer.Upstream.OutputTokens),
			int64(buffer.Upstream.CacheWriteTokens),
			int64(buffer.Upstream.CacheReadTokens),
			buffer.Upstream.CacheTTL
	}

	total := int64(buffer.GetRecordQuota() * 1000)
	inT := total / 2
	return inT, total - inT, 0, 0, ""
}

func clientChargeFor(chargeInst utils.Charge, buffer *utils.Buffer) float32 {
	if buffer.Upstream != nil {
		return utils.CountUpstreamQuota(chargeInst, buffer.Upstream)
	}
	return buffer.GetRecordQuota()
}

func readMarkupMultiplier(db *sql.DB) float32 {
	var v string
	if err := globals.QueryRowDb(db, `SELECT v FROM gtk_billing_config WHERE k='markup_multiplier'`).Scan(&v); err != nil {
		return defaultMarkupMultiplier
	}
	f, err := parseFloat32(v)
	if err != nil || f <= 0 {
		return defaultMarkupMultiplier
	}
	return f
}

func lookupActivePlanID(db *sql.DB, userID int64) sql.NullInt64 {
	var id sql.NullInt64
	err := globals.QueryRowDb(db, `
		SELECT plan_id FROM gtk_user_plan
		WHERE user_id = ? AND status = 'active'
		ORDER BY expire_at DESC LIMIT 1
	`, userID).Scan(&id)
	if err != nil {
		return sql.NullInt64{}
	}
	return id
}

func providerOf(model string) string {
	switch {
	case strings.HasPrefix(model, "claude-"):
		return "anthropic"
	case strings.HasPrefix(model, "deepseek-"):
		return "deepseek"
	case strings.HasPrefix(model, "gpt-"):
		return "openai"
	case strings.HasPrefix(model, "qwen-"):
		return "qwen"
	default:
		return ""
	}
}

func fallbackModel(chargeInst utils.Charge) string {
	models := chargeInst.GetModels()
	if len(models) == 0 {
		return ""
	}
	return models[0]
}

func nullableInt64(v sql.NullInt64) interface{} {
	if !v.Valid {
		return nil
	}
	return v.Int64
}

func parseFloat32(v string) (float32, error) {
	f, err := strconv.ParseFloat(v, 32)
	return float32(f), err
}
