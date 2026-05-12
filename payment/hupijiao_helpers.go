// Hupijiao (虎皮椒) payment helpers — sign, randomNonce, postForm, endpoint.
//
// Duplicated from service/checkout.go's unexported helpers because:
//   - service/checkout.go imports payment via dispatch_service.go (no cycle today)
//   - payment/hupijiao_checkout.go would create payment → service → payment cycle
//     if it called into the unexported service helpers via a public re-export.
//   - ~50 LOC of HMAC-MD5 + HTTP POST is light; keeping a self-contained copy
//     here ships v0.22 tonight without a broader refactor.
//
// Long-term: move to internal/hupijiao/ exposing Sign/PostForm and have both
// service + payment depend on it. Tracked as PKG follow-up post-launch.
//
// HMAC-MD5 algorithm: same wire-compatible implementation as
// service/checkout.go::hupijiaoSign — sort params alphabetically, concat
// k=v&k=v, append merchant_secret, MD5, lowercase hex. xunhupay.com docs
// mandate MD5; preimage resistance suffices for HMAC-style signed payloads
// even though MD5 is broken for collision resistance.

package payment

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const defaultHupijiaoEndpoint = "https://api.xunhupay.com/payment/do.html"

// hupijiaoSign computes HMAC-MD5 over alphabetically-sorted k=v pairs
// joined by &, with merchant_secret appended, per hupijiao docs. The
// `hash` field itself is excluded from signing.
func hupijiaoSign(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "hash" {
			continue
		}
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

// randomNonce returns a 16-char hex nonce for hupijiao request anti-replay.
// Crypto-rand sourced; falls back to time-based if rand fails (extremely
// unlikely on Linux production hosts but keeps tests deterministic-friendly).
func randomNonce() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Defensive fallback — collision-tolerant since signature itself
		// is the actual security boundary. Format avoids dashes.
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// hupijiaoResponse is what xunhupay.com/payment/do.html returns. Fields
// outside what we use are intentionally elided.
type hupijiaoResponse struct {
	ErrCode   int    `json:"errcode"`
	ErrMsg    string `json:"errmsg"`
	URL       string `json:"url"`        // alipay native deep-link
	URLQRCode string `json:"url_qrcode"` // PNG image URL
}

// postForm POSTs application/x-www-form-urlencoded to endpoint and
// decodes a hupijiaoResponse. 10s timeout — hupijiao is usually <1s
// but their gateway can stall under load; we'd rather fail than hang
// the customer's checkout indefinitely.
func postForm(endpoint string, params map[string]string) (*hupijiaoResponse, error) {
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(endpoint,
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("hupijiao POST: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("hupijiao read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hupijiao non-200: status=%d body=%s",
			resp.StatusCode, truncate(string(body), 256))
	}

	var out hupijiaoResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("hupijiao json decode: %w (body=%s)",
			err, truncate(string(body), 256))
	}
	return &out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
