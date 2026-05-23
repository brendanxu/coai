// public_pricing.go — PKG-PRICING-DYNAMIC public endpoint.
//
// Surface:
//
//	GET /api/gtk/v1/pricing   NO AUTH. Returns model rows for the public
//	                          Pricing.tsx rate table, sourced from
//	                          gtk_provider_pricing display_* columns.
//
// All-or-nothing rule: only rows where ALL 7 display_* fields are NOT NULL
// appear in the response. This means the admin can suppress a model from
// the public page simply by leaving any one display field NULL.
//
// The response shape matches Pricing.tsx ModelRow exactly so the frontend
// can drop-in replace the hardcoded MODEL_ROWS array with a fetch.

package newapi

import (
	"chat/connection"
	"chat/globals"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// PublicModelRow is the JSON shape returned by GET /gtk/v1/pricing.
// Field names and types mirror Pricing.tsx ModelRow exactly.
type PublicModelRow struct {
	Model          string `json:"model"`
	Vendor         string `json:"vendor"`
	Context        string `json:"context"`
	PriceIn        string `json:"priceIn"`
	PriceOut       string `json:"priceOut"`
	CreditsPerMOut string `json:"creditsPerMOut"`
	Cache          string `json:"cache"` // "true" | "cache_control" | "false"
}

// GetPublicPricingAPI handles GET /api/gtk/v1/pricing.
// No auth required — this is the public pricing reference table.
//
// Selects from gtk_provider_pricing WHERE token_type='input' AND all 7
// display_* fields are NOT NULL, ordered by display_credits_per_m DESC
// (highest-credit model first, matching the original Pricing.tsx ordering).
//
// Graceful degradation: on DB error returns 500 with success:false and a
// human-readable message. The frontend must handle this without white-screening.
func GetPublicPricingAPI(c *gin.Context) {
	models, err := loadPublicPricingRows(connection.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "pricing data unavailable: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"models": models,
		},
	})
}

// loadPublicPricingRows queries gtk_provider_pricing for the latest row per
// provider/model with a complete display column set (all 7 NOT NULL). Returns
// an empty slice (not nil) when no rows qualify so the JSON response is []
// rather than null.
func loadPublicPricingRows(db *sql.DB) ([]PublicModelRow, error) {
	rows, err := globals.QueryDb(db, `
		SELECT p.display_name, p.vendor_label, p.context_size,
		       p.display_in_cny_per_m, p.display_out_cny_per_m,
		       p.display_credits_per_m, p.cache_flag
		FROM gtk_provider_pricing p
		WHERE p.token_type = 'input'
		  AND p.display_in_cny_per_m  IS NOT NULL
		  AND p.display_out_cny_per_m IS NOT NULL
		  AND p.display_credits_per_m IS NOT NULL
		  AND p.display_name          IS NOT NULL
		  AND p.vendor_label          IS NOT NULL
		  AND p.context_size          IS NOT NULL
		  AND p.cache_flag            IS NOT NULL
		  AND p.id = (
		    SELECT p2.id
		    FROM gtk_provider_pricing p2
		    WHERE p2.provider = p.provider
		      AND p2.model_id = p.model_id
		      AND p2.token_type = 'input'
		      AND p2.display_in_cny_per_m  IS NOT NULL
		      AND p2.display_out_cny_per_m IS NOT NULL
		      AND p2.display_credits_per_m IS NOT NULL
		      AND p2.display_name          IS NOT NULL
		      AND p2.vendor_label          IS NOT NULL
		      AND p2.context_size          IS NOT NULL
		      AND p2.cache_flag            IS NOT NULL
		    ORDER BY p2.effective_from DESC, p2.id DESC
		    LIMIT 1
		  )
		ORDER BY p.display_credits_per_m DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query public pricing: %w", err)
	}
	defer rows.Close()

	out := make([]PublicModelRow, 0, 8)
	for rows.Next() {
		var displayName, vendorLabel, contextSize, cacheFlag string
		var priceIn, priceOut float64
		var creditsPerM int64

		if err := rows.Scan(
			&displayName, &vendorLabel, &contextSize,
			&priceIn, &priceOut, &creditsPerM, &cacheFlag,
		); err != nil {
			return nil, fmt.Errorf("scan public pricing row: %w", err)
		}

		out = append(out, PublicModelRow{
			Model:          displayName,
			Vendor:         vendorLabel,
			Context:        contextSize,
			PriceIn:        fmt.Sprintf("%.2f", priceIn),
			PriceOut:       fmt.Sprintf("%.2f", priceOut),
			CreditsPerMOut: strconv.FormatInt(creditsPerM, 10),
			Cache:          cacheFlag,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iter public pricing rows: %w", err)
	}
	return out, nil
}
