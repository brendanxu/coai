package usage

import (
	"chat/auth"
	"chat/channel"
	"chat/globals"
	"chat/plans"
	"chat/utils"
	"database/sql"
	"math"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

type usageLogRow struct {
	UserID            int64
	PlanID            sql.NullInt64
	Service           string
	TokensUsed        int64
	CostCents         int64
	ModelID           string
	Provider          string
	InputTokens       int64
	OutputTokens      int64
	CacheWriteTokens  int64
	CacheReadTokens   int64
	CacheTTL          string
	UpstreamCostMicro int64
	ClientChargeMicro int64
	MarkupMultiplier  float64
}

func newWriterDB(t *testing.T) *sql.DB {
	t.Helper()
	prevSQLite := globals.SqliteEngine
	prevCharge := channel.ChargeInstance
	globals.SqliteEngine = true
	t.Cleanup(func() {
		globals.SqliteEngine = prevSQLite
		channel.ChargeInstance = prevCharge
	})

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`
		CREATE TABLE auth (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  username TEXT UNIQUE
		)
	`); err != nil {
		t.Fatalf("create auth: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO auth (id, username) VALUES (42, 'writer')`); err != nil {
		t.Fatalf("insert auth: %v", err)
	}
	if err := plans.Migrate(db); err != nil {
		t.Fatalf("plans migrate: %v", err)
	}

	channel.ChargeInstance = &channel.ChargeManager{
		Models: map[string]*channel.Charge{},
	}
	return db
}

func setCharge(model string, c *channel.Charge) {
	c.Models = []string{model}
	channel.ChargeInstance.Models[model] = c
}

func tokenCharge() *channel.Charge {
	return &channel.Charge{
		Type:         globals.TokenBilling,
		Input:        0.04,
		Output:       0.20,
		CacheRead:    0.005,
		CacheWrite5m: 0.05,
		CacheWrite1h: 0.08,
	}
}

func writerUser() *auth.User {
	return &auth.User{ID: 42, Username: "writer"}
}

func fetchUsageLog(t *testing.T, db *sql.DB) usageLogRow {
	t.Helper()
	var row usageLogRow
	err := db.QueryRow(`
		SELECT user_id, plan_id, service, tokens_used, cost_cents,
		       model_id, provider, input_tokens, output_tokens,
		       cache_write_tokens, cache_read_tokens, cache_ttl,
		       upstream_cost_micro, client_charge_micro, markup_multiplier
		FROM gtk_app_usage_log
		ORDER BY id DESC LIMIT 1
	`).Scan(
		&row.UserID, &row.PlanID, &row.Service, &row.TokensUsed, &row.CostCents,
		&row.ModelID, &row.Provider, &row.InputTokens, &row.OutputTokens,
		&row.CacheWriteTokens, &row.CacheReadTokens, &row.CacheTTL,
		&row.UpstreamCostMicro, &row.ClientChargeMicro, &row.MarkupMultiplier,
	)
	if err != nil {
		t.Fatalf("fetch usage log: %v", err)
	}
	return row
}

func TestWriteUsageLog_HappyPathWithUpstreamUsage(t *testing.T) {
	db := newWriterDB(t)
	charge := tokenCharge()
	setCharge("claude-sonnet-4-5", charge)

	buffer := &utils.Buffer{
		Model:             "claude-sonnet-4-5",
		Charge:            charge,
		PreferredCacheTTL: "5m",
	}
	buffer.RecordUpstreamUsage(&globals.UpstreamUsage{
		InputTokens:      1000,
		OutputTokens:     500,
		CacheWriteTokens: 300,
		CacheReadTokens:  200,
	})

	if err := WriteUsageLog(db, writerUser(), buffer, buffer.Model); err != nil {
		t.Fatalf("WriteUsageLog: %v", err)
	}

	row := fetchUsageLog(t, db)
	if row.InputTokens != 1000 || row.OutputTokens != 500 ||
		row.CacheWriteTokens != 300 || row.CacheReadTokens != 200 {
		t.Fatalf("token fields: got in=%d out=%d cw=%d cr=%d",
			row.InputTokens, row.OutputTokens, row.CacheWriteTokens, row.CacheReadTokens)
	}
	if row.CacheTTL != "5m" {
		t.Fatalf("cache_ttl: got %q want 5m", row.CacheTTL)
	}
	if row.TokensUsed != 2000 {
		t.Fatalf("tokens_used: got %d want 2000", row.TokensUsed)
	}
	if row.ModelID != "claude-sonnet-4-5" || row.Provider != "anthropic" {
		t.Fatalf("model/provider: got %q/%q", row.ModelID, row.Provider)
	}
}

func TestWriteUsageLog_FallbackPathNoUpstream(t *testing.T) {
	db := newWriterDB(t)
	charge := tokenCharge()
	setCharge("gpt-4o", charge)

	buffer := &utils.Buffer{
		Model:  "gpt-4o",
		Quota:  0.123,
		Charge: charge,
	}
	if err := WriteUsageLog(db, writerUser(), buffer, buffer.Model); err != nil {
		t.Fatalf("WriteUsageLog: %v", err)
	}

	row := fetchUsageLog(t, db)
	if row.InputTokens != 61 || row.OutputTokens != 62 {
		t.Fatalf("fallback split: got %d/%d want 61/62", row.InputTokens, row.OutputTokens)
	}
	if row.CacheWriteTokens != 0 || row.CacheReadTokens != 0 || row.CacheTTL != "" {
		t.Fatalf("fallback cache fields: cw=%d cr=%d ttl=%q",
			row.CacheWriteTokens, row.CacheReadTokens, row.CacheTTL)
	}
}

func TestWriteUsageLog_MarkupMath(t *testing.T) {
	db := newWriterDB(t)
	charge := tokenCharge()
	setCharge("claude-sonnet-4-5", charge)
	buffer := upstreamBuffer("claude-sonnet-4-5", charge)

	if err := WriteUsageLog(db, writerUser(), buffer, buffer.Model); err != nil {
		t.Fatalf("WriteUsageLog: %v", err)
	}

	row := fetchUsageLog(t, db)
	client := float64(row.ClientChargeMicro) / 1_000_000
	upstream := float64(row.UpstreamCostMicro) / 1_000_000
	if math.Abs((client/row.MarkupMultiplier)-upstream) > 1e-5 {
		t.Fatalf("markup math: client=%f markup=%f upstream=%f", client, row.MarkupMultiplier, upstream)
	}
}

func TestWriteUsageLog_MarkupMultiplierOverride(t *testing.T) {
	db := newWriterDB(t)
	charge := tokenCharge()
	setCharge("claude-sonnet-4-5", charge)
	if _, err := db.Exec(`UPDATE gtk_billing_config SET v = '1.500' WHERE k = 'markup_multiplier'`); err != nil {
		t.Fatalf("update markup: %v", err)
	}

	if err := WriteUsageLog(db, writerUser(), upstreamBuffer("claude-sonnet-4-5", charge), ""); err != nil {
		t.Fatalf("WriteUsageLog: %v", err)
	}

	row := fetchUsageLog(t, db)
	if math.Abs(row.MarkupMultiplier-1.5) > 1e-9 {
		t.Fatalf("markup: got %f want 1.5", row.MarkupMultiplier)
	}
	client := float64(row.ClientChargeMicro) / 1_000_000
	upstream := float64(row.UpstreamCostMicro) / 1_000_000
	if math.Abs((client/1.5)-upstream) > 1e-5 {
		t.Fatalf("override math: client=%f upstream=%f", client, upstream)
	}
}

func TestWriteUsageLog_MarkupMultiplierMissingFallsBack(t *testing.T) {
	db := newWriterDB(t)
	charge := tokenCharge()
	setCharge("claude-sonnet-4-5", charge)
	if _, err := db.Exec(`DELETE FROM gtk_billing_config`); err != nil {
		t.Fatalf("delete markup: %v", err)
	}

	if err := WriteUsageLog(db, writerUser(), upstreamBuffer("claude-sonnet-4-5", charge), ""); err != nil {
		t.Fatalf("WriteUsageLog: %v", err)
	}

	row := fetchUsageLog(t, db)
	if math.Abs(row.MarkupMultiplier-1.3) > 1e-6 {
		t.Fatalf("markup fallback: got %f want 1.3", row.MarkupMultiplier)
	}
}

func TestWriteUsageLog_NullPlanIDWhenNoActivePlan(t *testing.T) {
	db := newWriterDB(t)
	charge := tokenCharge()
	setCharge("gpt-4o", charge)

	if err := WriteUsageLog(db, writerUser(), upstreamBuffer("gpt-4o", charge), ""); err != nil {
		t.Fatalf("WriteUsageLog: %v", err)
	}

	row := fetchUsageLog(t, db)
	if row.PlanID.Valid {
		t.Fatalf("plan_id: got %d want NULL", row.PlanID.Int64)
	}
}

func TestWriteUsageLog_ActivePlanResolution(t *testing.T) {
	db := newWriterDB(t)
	charge := tokenCharge()
	setCharge("gpt-4o", charge)
	seedPlans(t, db)

	if err := WriteUsageLog(db, writerUser(), upstreamBuffer("gpt-4o", charge), ""); err != nil {
		t.Fatalf("WriteUsageLog: %v", err)
	}

	row := fetchUsageLog(t, db)
	if !row.PlanID.Valid || row.PlanID.Int64 != 2 {
		t.Fatalf("plan_id: got %+v want 2", row.PlanID)
	}
}

func TestWriteUsageLog_ProviderInference(t *testing.T) {
	cases := []struct {
		model    string
		provider string
	}{
		{"claude-sonnet-4-5", "anthropic"},
		{"gpt-4o", "openai"},
		{"deepseek-v3", "deepseek"},
		{"unknown-model", ""},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			db := newWriterDB(t)
			charge := tokenCharge()
			setCharge(tc.model, charge)
			if err := WriteUsageLog(db, writerUser(), upstreamBuffer(tc.model, charge), ""); err != nil {
				t.Fatalf("WriteUsageLog: %v", err)
			}
			row := fetchUsageLog(t, db)
			if row.Provider != tc.provider {
				t.Fatalf("provider: got %q want %q", row.Provider, tc.provider)
			}
		})
	}
}

func upstreamBuffer(model string, charge *channel.Charge) *utils.Buffer {
	return &utils.Buffer{
		Model:  model,
		Charge: charge,
		Upstream: &globals.UpstreamUsage{
			InputTokens:      1000,
			OutputTokens:     500,
			CacheWriteTokens: 300,
			CacheReadTokens:  200,
			CacheTTL:         "5m",
		},
	}
}

func seedPlans(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO gtk_plan (id, code, name, type, price_cents, duration_days, quota_config, is_active)
		VALUES
		  (1, 'expired', 'Expired', 'subscription', 100, 30, '{}', 1),
		  (2, 'active', 'Active', 'subscription', 200, 30, '{}', 1)
	`); err != nil {
		t.Fatalf("insert gtk_plan: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO gtk_user_plan (user_id, plan_id, status, expire_at, remaining, order_id)
		VALUES
		  (42, 1, 'expired', '2026-01-01 00:00:00', '{}', 'order_expired'),
		  (42, 2, 'active', '2026-12-31 00:00:00', '{}', 'order_active')
	`); err != nil {
		t.Fatalf("insert gtk_user_plan: %v", err)
	}
}
