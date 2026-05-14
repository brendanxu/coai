// admin_options.go — NewAPI system option + model-ratio handlers for the GTK admin panel.
//
// Surface:
//
//   GET  /gtk/v1/admin/model-ratios    fetch ModelPrice option → decoded ratio map
//   PUT  /gtk/v1/admin/model-ratios    write updated ratio map → ModelPrice option
//   GET  /gtk/v1/admin/system-options  fetch all NewAPI options
//   PUT  /gtk/v1/admin/system-options  set one NewAPI option {key, value}
//
// NewAPI options GET: /api/option/ → {success, data: [{Key, Value}]}
// NewAPI options PUT: /api/option/ body {key: "...", value: "..."}

package newapi

import (
	"chat/auth"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

// newAPIOption mirrors one entry from NewAPI's option list.
type newAPIOption struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// newAPIOptionsEnvelope wraps the /api/option/ GET response.
// NewAPI returns data as an array (not a paginated struct).
type newAPIOptionsEnvelope struct {
	Success bool           `json:"success"`
	Message string         `json:"message,omitempty"`
	Data    []newAPIOption `json:"data"`
}

// GetModelRatiosAPI handles GET /gtk/v1/admin/model-ratios
//
// Fetches ModelPrice option from NewAPI and decodes it from a JSON-encoded
// string (e.g. `{"gpt-4o": 15, "claude-3-5-sonnet": 9}`) into a map.
func GetModelRatiosAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}

	cli, err := Default()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "newapi not configured: " + err.Error(),
		})
		return
	}

	var env newAPIOptionsEnvelope
	if err := cli.do(c.Request.Context(), "GET", "/api/option/", nil, 0, &env); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "fetch options failed: " + err.Error(),
		})
		return
	}
	if !env.Success {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "newapi: fetch options: " + env.Message,
		})
		return
	}

	// Find ModelPrice in the options list.
	var modelPriceRaw string
	for _, opt := range env.Data {
		if opt.Key == "ModelPrice" {
			modelPriceRaw = opt.Value
			break
		}
	}

	ratios := map[string]float64{}
	if modelPriceRaw != "" {
		if err := json.Unmarshal([]byte(modelPriceRaw), &ratios); err != nil {
			// Non-fatal: return empty map with a warning rather than 500.
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"data":    gin.H{"ratios": ratios},
				"warning": "ModelPrice option could not be parsed: " + err.Error(),
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"ratios": ratios},
	})
}

// UpdateModelRatiosRequest is the body for PUT /admin/model-ratios.
type UpdateModelRatiosRequest struct {
	Ratios map[string]float64 `json:"ratios"`
}

// UpdateModelRatiosAPI handles PUT /gtk/v1/admin/model-ratios
//
// Encodes the provided ratio map as a JSON string and writes it to NewAPI's
// ModelPrice option via PUT /api/option/.
func UpdateModelRatiosAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}

	var req UpdateModelRatiosRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request: " + err.Error(),
		})
		return
	}
	if req.Ratios == nil {
		req.Ratios = map[string]float64{}
	}

	encoded, err := json.Marshal(req.Ratios)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "marshal ratios failed: " + err.Error(),
		})
		return
	}

	cli, err := Default()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "newapi not configured: " + err.Error(),
		})
		return
	}

	body := struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}{Key: "ModelPrice", Value: string(encoded)}

	var env envelope[any]
	if err := cli.do(c.Request.Context(), "PUT", "/api/option/", body, 0, &env); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "update ModelPrice failed: " + err.Error(),
		})
		return
	}
	if !env.Success {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "newapi: update ModelPrice: " + env.Message,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetSystemOptionsAPI handles GET /gtk/v1/admin/system-options
//
// Returns all NewAPI options as a list of {key, value} pairs.
func GetSystemOptionsAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}

	cli, err := Default()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "newapi not configured: " + err.Error(),
		})
		return
	}

	var env newAPIOptionsEnvelope
	if err := cli.do(c.Request.Context(), "GET", "/api/option/", nil, 0, &env); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "fetch options failed: " + err.Error(),
		})
		return
	}
	if !env.Success {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "newapi: fetch options: " + env.Message,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"options": env.Data},
	})
}

// UpdateSystemOptionRequest is the body for PUT /admin/system-options.
type UpdateSystemOptionRequest struct {
	Key   string `json:"key" binding:"required"`
	Value string `json:"value"`
}

// UpdateSystemOptionAPI handles PUT /gtk/v1/admin/system-options
//
// Sets one NewAPI option by key. Body: {key: "PasswordLoginEnabled", value: "true"}.
func UpdateSystemOptionAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}

	var req UpdateSystemOptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request: " + err.Error(),
		})
		return
	}

	cli, err := Default()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "newapi not configured: " + err.Error(),
		})
		return
	}

	body := struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}{Key: req.Key, Value: req.Value}

	var env envelope[any]
	if err := cli.do(c.Request.Context(), "PUT", "/api/option/", body, 0, &env); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "update option failed: " + err.Error(),
		})
		return
	}
	if !env.Success {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": "newapi: update option: " + env.Message,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}
