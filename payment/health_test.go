package payment

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestHealthAPI_ReturnsBooleanOnly(t *testing.T) {
	r, _ := newTestEngine(t)
	r.GET("/payment/health", HealthAPI)

	req := httptest.NewRequest("GET", "/payment/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("got %d want 200", w.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unparseable response: %v; body=%s", err, w.Body.String())
	}
	if body["healthy"] != true {
		t.Fatalf("got healthy=%v want true", body["healthy"])
	}
	// Codex P2 fix (2026-04-27): no business telemetry leaked from the
	// public health probe. Counts and timestamps are explicitly NOT in
	// the response.
	if _, has := body["processed_total"]; has {
		t.Errorf("processed_total leaked from public health endpoint: %v", body["processed_total"])
	}
	if _, has := body["last_event_at"]; has {
		t.Errorf("last_event_at leaked from public health endpoint: %v", body["last_event_at"])
	}
}

func TestCleanupOldEvents_RemovesPast90d(t *testing.T) {
	_, db := newTestEngine(t)

	// Two rows: one 100d old (should be deleted), one 30d old (should survive).
	if _, err := db.Exec(`
		INSERT INTO gtk_webhook_event (event_id, event_type, received_at)
		VALUES
		  ('evt_old', 'subscription_created', datetime('now', '-100 days')),
		  ('evt_recent', 'subscription_created', datetime('now', '-30 days'))
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cleanupOldEvents(db)

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gtk_webhook_event WHERE event_id = 'evt_old'`).Scan(&count); err != nil {
		t.Fatalf("query old: %v", err)
	}
	if count != 0 {
		t.Fatalf("evt_old should be cleaned up; got count=%d", count)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM gtk_webhook_event WHERE event_id = 'evt_recent'`).Scan(&count); err != nil {
		t.Fatalf("query recent: %v", err)
	}
	if count != 1 {
		t.Fatalf("evt_recent should survive; got count=%d", count)
	}
}
