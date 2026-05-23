package newapi

import "testing"

func TestLoadPublicPricingRows_DedupesDuplicateDisplayRows(t *testing.T) {
	db := planTestDB(t)

	seedPublicPricingRow := func(provider, modelID, effectiveFrom, displayName string, credits int64) {
		t.Helper()
		if _, err := db.Exec(`
			INSERT INTO gtk_provider_pricing
			  (provider, model_id, token_type, upstream_per_m, effective_from,
			   display_in_cny_per_m, display_out_cny_per_m, display_credits_per_m,
			   display_name, vendor_label, context_size, cache_flag)
			VALUES (?, ?, 'input', 1.0, ?, 18.20, 72.80, ?, ?, ?, '128k', 'true')
		`, provider, modelID, effectiveFrom, credits, displayName, provider); err != nil {
			t.Fatalf("seed pricing row %s/%s/%s: %v", provider, modelID, effectiveFrom, err)
		}
	}

	seedPublicPricingRow("openai", "gpt-4o", "2026-05-20 00:00:00", "GPT-4o stale", 3640)
	seedPublicPricingRow("openai", "gpt-4o", "2026-05-21 00:00:00", "GPT-4o latest", 3650)
	seedPublicPricingRow("deepseek", "deepseek-v3", "2026-05-21 00:00:00", "DeepSeek V3", 200)

	got, err := loadPublicPricingRows(db)
	if err != nil {
		t.Fatalf("loadPublicPricingRows: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2: %#v", len(got), got)
	}
	if got[0].Model != "GPT-4o latest" {
		t.Fatalf("first row = %q, want latest GPT-4o row", got[0].Model)
	}
	for _, row := range got {
		if row.Model == "GPT-4o stale" {
			t.Fatalf("stale duplicate row leaked into public pricing: %#v", got)
		}
	}
}
