// Package relay implements the OpenAI-compatible /v1/* reverse-proxy handler.
//
// PKG-A-4 (2026-05-18): aidesk (and any OpenAI-SDK client) can point at
// https://www.greentokey.com with a greentokey sk-xxx API key. The handler:
//
//  1. Authenticates the request via the Bearer sk-xxx in Authorization header
//     (AuthMiddleware already ran; handler reads the parsed user from context).
//  2. Looks up gtk_newapi_binding for the coai_user_id → newapi_user_id.
//  3. Reverse-proxies the request to NewAPI :3000, substituting:
//     Authorization: Bearer <newapi.relay_token>
//  4. Streams the response back verbatim (SSE / chunked transfer).
//
// Three-layer isolation invariant preserved: NewAPI :3000 is NEVER exposed
// on a public Caddy route. This handler is the only internal bridge.
package relay

import (
	"database/sql"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"chat/newapi"
	"chat/utils"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// relayHTTPClient is a shared transport for upstream NewAPI calls.
// DisableCompression: we stream raw bytes; let NewAPI decide encoding.
// No timeout on the client level (SSE streams can be long); individual
// request context carries the caller's deadline.
var (
	relayHTTPClient     *http.Client
	relayHTTPClientOnce sync.Once
)

func getRelayHTTPClient() *http.Client {
	relayHTTPClientOnce.Do(func() {
		relayHTTPClient = &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 60 * time.Second,
				}).DialContext,
				MaxIdleConns:          50,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				DisableCompression:    true, // preserve upstream encoding for streaming
			},
			Timeout: 0, // no client-level timeout; SSE streams are unbounded
		}
	})
	return relayHTTPClient
}

// relayToken returns the NewAPI sk-xxx to use for /v1 relay.
// Priority: newapi.relay_token (explicit config) → newapi.admin_access_token
// (fallback with warning — NewAPI /v1 expects a sk-xxx not the admin token).
func relayToken() string {
	if t := viper.GetString("newapi.relay_token"); t != "" {
		return t
	}
	fallback := viper.GetString("newapi.admin_access_token")
	if fallback != "" {
		log.Printf("[relay] WARN: newapi.relay_token not set; falling back to admin_access_token — NewAPI /v1 may reject non-sk-xxx tokens. Set newapi.relay_token in config.")
	}
	return fallback
}

// newAPIBase returns the configured NewAPI base URL (default: http://newapi:3000).
// Variable indirection allows tests to override via newAPIBaseForTest.
var newAPIBase = func() string {
	u := viper.GetString("newapi.base_url")
	if u == "" {
		return "http://newapi:3000"
	}
	return strings.TrimRight(u, "/")
}

// HandleRelay is the gin.HandlerFunc for engine.Any("/v1/*path", relay.HandleRelay).
//
// It expects AuthMiddleware to have already run (sets "user" and "auth" in context).
// Unauthenticated requests get 401 before any upstream call.
func HandleRelay(c *gin.Context) {
	// 1. Check authentication: AuthMiddleware sets "auth" = true when a valid
	//    sk-xxx was presented. If auth is false the request has no valid credentials.
	if auth, _ := c.Get("auth"); auth != true {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{
				"message": "No valid API key provided. Supply a greentokey sk-xxx as Bearer token.",
				"type":    "invalid_request_error",
				"code":    "invalid_api_key",
			},
		})
		return
	}

	// 2. Resolve the coai user_id from context.
	//    AuthMiddleware stores the username; we need the numeric ID for the binding lookup.
	db := utils.GetDBFromContext(c)
	username, _ := c.Get("user")
	usernameStr, _ := username.(string)
	if usernameStr == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{
				"message": "Could not resolve user identity.",
				"type":    "internal_error",
				"code":    "user_resolution_failed",
			},
		})
		return
	}

	var coaiUserID int64
	if err := db.QueryRow("SELECT id FROM auth WHERE username = ?", usernameStr).Scan(&coaiUserID); err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{
				"message": "User not found.",
				"type":    "internal_error",
				"code":    "user_not_found",
			},
		})
		return
	}

	// 3. Look up gtk_newapi_binding for this user.
	bind, err := newapi.LoadBinding(db, coaiUserID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"message": "No upstream binding for this account. Please purchase a token plan first.",
					"type":    "invalid_request_error",
					"code":    "no_upstream_binding",
				},
			})
			return
		}
		log.Printf("[relay] LoadBinding error for coai_user_id=%d: %v", coaiUserID, err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": "Failed to load upstream binding.",
				"type":    "internal_error",
				"code":    "binding_load_error",
			},
		})
		return
	}
	_ = bind // bind.NewapiUserID available for future usage logging

	// 4. Build the upstream URL: <newapi_base>/v1/<path>
	path := c.Param("path") // includes leading slash: "/chat/completions"
	if path == "" {
		path = "/"
	}
	upstreamURL := newAPIBase() + "/v1" + path

	// Preserve query string (e.g. ?model=xxx)
	if rawQuery := c.Request.URL.RawQuery; rawQuery != "" {
		upstreamURL += "?" + rawQuery
	}

	// 5. Build upstream request, forwarding body verbatim.
	upstreamReq, err := http.NewRequestWithContext(
		c.Request.Context(),
		c.Request.Method,
		upstreamURL,
		c.Request.Body,
	)
	if err != nil {
		log.Printf("[relay] build upstream request error: %v", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "Failed to build upstream request.", "type": "internal_error"},
		})
		return
	}

	// 6. Copy safe request headers; rewrite Authorization to relay_token.
	copyRequestHeaders(c.Request.Header, upstreamReq.Header)
	upstreamReq.Header.Set("Authorization", "Bearer "+relayToken())

	// 7. Execute upstream request.
	upstreamResp, err := getRelayHTTPClient().Do(upstreamReq)
	if err != nil {
		log.Printf("[relay] upstream error (user=%d, path=%s): %v", coaiUserID, path, err)
		c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"message": "Upstream gateway error.",
				"type":    "api_error",
				"code":    "upstream_error",
			},
		})
		return
	}
	defer upstreamResp.Body.Close()

	// 8. Copy response headers back to caller (before WriteHeader).
	copyResponseHeaders(upstreamResp.Header, c.Writer.Header())

	// 9. Write status code + stream body verbatim.
	//    For SSE (text/event-stream), we flush after each chunk so the client
	//    receives events immediately rather than waiting for the full body.
	c.Writer.WriteHeader(upstreamResp.StatusCode)

	if flusher, ok := c.Writer.(http.Flusher); ok {
		buf := make([]byte, 4096)
		for {
			n, readErr := upstreamResp.Body.Read(buf)
			if n > 0 {
				if _, writeErr := c.Writer.Write(buf[:n]); writeErr != nil {
					// Client disconnected — normal for long SSE streams.
					break
				}
				flusher.Flush()
			}
			if readErr != nil {
				if readErr != io.EOF {
					log.Printf("[relay] stream read error (user=%d, path=%s): %v", coaiUserID, path, readErr)
				}
				break
			}
		}
	} else {
		if _, copyErr := io.Copy(c.Writer, upstreamResp.Body); copyErr != nil {
			log.Printf("[relay] copy response error (user=%d, path=%s): %v", coaiUserID, path, copyErr)
		}
	}
}

// hopByHopHeaders must NOT be forwarded between proxies (RFC 7230 §6.1).
var hopByHopHeaders = map[string]struct{}{
	"Connection":          {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailers":            {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

// copyRequestHeaders copies non-hop-by-hop headers from src to dst,
// skipping Authorization (rewritten by caller) and Host (set by http.Client).
func copyRequestHeaders(src, dst http.Header) {
	for k, vv := range src {
		if _, skip := hopByHopHeaders[k]; skip {
			continue
		}
		if strings.EqualFold(k, "Authorization") {
			continue // rewritten to relay_token by caller
		}
		if strings.EqualFold(k, "Host") {
			continue // set by http.Client from url
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// copyResponseHeaders copies non-hop-by-hop response headers from src to dst.
func copyResponseHeaders(src, dst http.Header) {
	for k, vv := range src {
		if _, skip := hopByHopHeaders[k]; skip {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// Register mounts OpenAI-compatible /v1/* endpoints on the engine root
// (NOT under /api group). Matches aidesk expectation:
// POST https://www.greentokey.com/v1/chat/completions
//
// We register specific endpoints (not a /v1/*path wildcard) to avoid
// conflicting with /v1/usage/me which usage/handler.go owns. Add new
// OpenAI endpoints here as aidesk / other clients need them.
//
// Must be called AFTER middleware.RegisterMiddleware (which sets up AuthMiddleware).
func Register(engine *gin.Engine) {
	openAIEndpoints := []string{
		"/v1/chat/completions",
		"/v1/completions",
		"/v1/embeddings",
		"/v1/models",
		"/v1/images/generations",
		"/v1/audio/transcriptions",
		"/v1/audio/translations",
		"/v1/audio/speech",
		"/v1/moderations",
	}
	for _, path := range openAIEndpoints {
		engine.Any(path, HandleRelay)
	}
}
