package payment

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// Tests covering the v0.6.a10 codex review fixes (P1 + P2 findings).
// Each test names the codex finding it locks in.

// --- P1-A: idempotency state machine -----------------------------------------

func TestClassifyEvent_FreshFirstTime(t *testing.T) {
	_, db := newTestEngine(t)

	cls, err := classifyEvent(db, "evt_first", "subscription_created")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cls != eventClassFresh {
		t.Fatalf("got class=%v want eventClassFresh", cls)
	}
}

func TestClassifyEvent_InFlightWhenProcessedAtIsNull(t *testing.T) {
	// Codex P1-A: dup-key + processed_at NULL = first attempt is mid-flight
	// (or crashed); LS should retry, not silently 200.
	_, db := newTestEngine(t)

	if _, err := classifyEvent(db, "evt_inflight", "subscription_created"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Don't update processed_at — simulate crash / in-flight state.

	cls, err := classifyEvent(db, "evt_inflight", "subscription_created")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cls != eventClassInFlight {
		t.Fatalf("got class=%v want eventClassInFlight", cls)
	}
}

func TestClassifyEvent_ProcessedWhenProcessedAtIsSet(t *testing.T) {
	// Dup-key + processed_at set = safe duplicate; ack 200.
	_, db := newTestEngine(t)

	if _, err := classifyEvent(db, "evt_done", "subscription_created"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := db.Exec(`UPDATE gtk_webhook_event SET processed_at = ? WHERE event_id = ?`,
		"2026-04-27 12:00:00", "evt_done"); err != nil {
		t.Fatalf("set processed_at: %v", err)
	}

	cls, err := classifyEvent(db, "evt_done", "subscription_created")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cls != eventClassProcessed {
		t.Fatalf("got class=%v want eventClassProcessed", cls)
	}
}

func TestHandleWebhook_InFlightRetryReturns503(t *testing.T) {
	// Full integration: row exists with NULL processed_at, second webhook
	// for same body should NOT 200 — must 503 so LS retries.
	r, db := newTestEngine(t)
	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	body := samplePayload("subscription_created")
	eventID := sha256Hex(body)

	// Pre-seed an in-flight row (mid-dispatch state simulated).
	if _, err := db.Exec(`
		INSERT INTO gtk_webhook_event (event_id, event_type) VALUES (?, ?)
	`, eventID, "subscription_created"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(body))
	req.Header.Set("X-Signature", computeHMAC(body, secret))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 503 {
		t.Fatalf("got %d want 503 (in-flight retry); body=%s", w.Code, w.Body.String())
	}
}

// --- P1-B: monotonic guard against out-of-order webhooks ---------------------

func TestActivateExternalSubscription_DoesNotRegressExpiredAt(t *testing.T) {
	// Codex P1-B: an older webhook arriving last must NOT clobber a newer
	// expired_at already in the DB.
	_, db := newTestEngine(t)

	newer := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	older := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	if err := activateExternalSubscription(db, 42, levelStarter, newer); err != nil {
		t.Fatalf("first activation: %v", err)
	}
	// Now the "older" webhook lands.
	if err := activateExternalSubscription(db, 42, levelStarter, older); err != nil {
		t.Fatalf("older activation: %v", err)
	}

	var got string
	if err := db.QueryRow(`SELECT expired_at FROM subscription WHERE user_id = 42`).Scan(&got); err != nil {
		t.Fatalf("read: %v", err)
	}
	wantPrefix := "2026-06-01"
	if got[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("got expired_at=%q, expected newer 2026-06-01… (older webhook regressed state)", got)
	}
}

// --- P1-C: malformed JSON returns 500 (not 200) so LS retries ---------------

func TestHandleWebhook_MalformedJSON_Returns500(t *testing.T) {
	// Codex P1-C: signed but unparseable body must NOT 200 — that silently
	// loses retriable provider bugs. Return 500 so LS retries.
	r, _ := newTestEngine(t)
	secret := "test-webhook-secret"
	viper.Set("lemonsqueezy.webhook_secret", secret)
	t.Cleanup(func() { viper.Set("lemonsqueezy.webhook_secret", "") })

	garbage := []byte(`{"meta": INVALID JSON`)
	req := httptest.NewRequest("POST", "/webhook/lemonsqueezy", bytes.NewReader(garbage))
	req.Header.Set("X-Signature", computeHMAC(garbage, secret))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Fatalf("got %d want 500 (malformed JSON should trigger retry); body=%s",
			w.Code, w.Body.String())
	}
}

// --- P2-D: health endpoint discloses no business telemetry -------------------

func TestHealthAPI_NoCountFields(t *testing.T) {
	// Codex P2-D: previous version exposed processed_total + last_event_at.
	// Public uptime probes only need the boolean.
	r, _ := newTestEngine(t)
	r.GET("/payment/health", HealthAPI)

	req := httptest.NewRequest("GET", "/payment/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var body map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &body)

	for _, leaked := range []string{"processed_total", "last_event_at", "count"} {
		if _, has := body[leaked]; has {
			t.Errorf("health response leaked field %q (value=%v)", leaked, body[leaked])
		}
	}
}

// --- P2-E: store_slug + variant_id format validation -------------------------

func TestBuildCheckoutURL_RejectsMalformedSlug(t *testing.T) {
	// Codex P2-E: slug is operator-controlled; validate to defend against
	// `evil.com/`, `..`, `#`, etc. that would warp URL parsing.
	viper.Set("lemonsqueezy.variant_id", "999999")
	t.Cleanup(func() { viper.Set("lemonsqueezy.variant_id", "") })

	bad := []string{
		"evil.com/",
		"foo@evil.com",
		"foo bar",
		"-leading-hyphen",
		"trailing-hyphen-",
		"UPPERCASE",
		"slash/inside",
		"hash#tag",
		"",
	}
	for _, s := range bad {
		viper.Set("lemonsqueezy.store_slug", s)
		if _, err := buildCheckoutURL(42, "", ""); err == nil {
			t.Errorf("buildCheckoutURL accepted bad slug %q", s)
		}
	}

	// Sanity: a well-formed slug still works.
	viper.Set("lemonsqueezy.store_slug", "greentokey")
	if _, err := buildCheckoutURL(42, "", ""); err != nil {
		t.Errorf("good slug rejected: %v", err)
	}
}

func TestBuildCheckoutURL_RejectsMalformedVariantID(t *testing.T) {
	viper.Set("lemonsqueezy.store_slug", "greentokey")
	t.Cleanup(func() { viper.Set("lemonsqueezy.store_slug", "") })

	bad := []string{
		"abc",
		"-1",
		"0",
		"1.5",
		"1e10",
		"99999999999999999999", // overflow region
	}
	for _, v := range bad {
		viper.Set("lemonsqueezy.variant_id", v)
		if _, err := buildCheckoutURL(42, "", ""); err == nil {
			t.Errorf("buildCheckoutURL accepted bad variant_id %q", v)
		}
	}
}
