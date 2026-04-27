package payment

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestHealthAPI_EmptyTable(t *testing.T) {
	r, _ := newTestEngine(t)

	req := httptest.NewRequest("GET", "/payment/health", nil)
	w := httptest.NewRecorder()
	r.GET("/payment/health", HealthAPI)
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
	// processed_total comes through JSON as float64.
	if body["processed_total"].(float64) != 0 {
		t.Fatalf("got processed_total=%v want 0", body["processed_total"])
	}
}

func TestHealthAPI_AfterProcessedEvent(t *testing.T) {
	r, db := newTestEngine(t)
	r.GET("/payment/health", HealthAPI)

	// Insert a fake processed event.
	if _, err := db.Exec(`
		INSERT INTO gtk_webhook_event (event_id, event_type, processed_at)
		VALUES ('evt_test', 'subscription_created', '2026-04-27 12:34:56')
	`); err != nil {
		t.Fatalf("seed event: %v", err)
	}

	req := httptest.NewRequest("GET", "/payment/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("got %d want 200; body=%s", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &body)

	if body["processed_total"].(float64) != 1 {
		t.Fatalf("got processed_total=%v want 1", body["processed_total"])
	}
	if body["last_event_at"].(string) == "" {
		t.Fatal("last_event_at should be populated after a processed event")
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
