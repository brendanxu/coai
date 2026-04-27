package carbon

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"

	"chat/globals"
)

//go:embed data/carbon_factors.json
var carbonFactorsJSON []byte

// FactorEntry is one row in the per-model coefficient table.
type FactorEntry struct {
	Model            string  `json:"model"`
	Region           string  `json:"region"`
	GCO2ePer1KTokens float64 `json:"gco2e_per_1k_tokens"`
	ValidFrom        string  `json:"valid_from"`
	Version          string  `json:"version"`
	Notes            string  `json:"notes,omitempty"`
}

// FactorsTable is the full JSON shape of carbon_factors.json. Mirrored by the
// /api/carbon/factors endpoint so the frontend Methodology page can render
// the same data.
type FactorsTable struct {
	Version            string        `json:"version"`
	ErrorMarginPct     int           `json:"error_margin_pct"`
	DefaultRegion      string        `json:"default_region"`
	Sources            []SourceEntry `json:"sources"`
	Factors            []FactorEntry `json:"factors"`
	WhatWeDontMeasure  []string      `json:"what_we_dont_measure"`
	CalibrationNote    string        `json:"calibration_note"`
}

type SourceEntry struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Note string `json:"note,omitempty"`
}

var (
	factorsTable FactorsTable
	factorsIdx   map[string]FactorEntry // key = lower(model) + "|" + lower(region)
	factorsOnce  sync.Once
)

// loadFactors parses the embedded JSON and builds the lookup index. Called
// lazily; safe to call from any goroutine.
func loadFactors() {
	if err := json.Unmarshal(carbonFactorsJSON, &factorsTable); err != nil {
		globals.Warn("carbon: failed to parse embedded carbon_factors.json: " + err.Error())
		return
	}
	factorsIdx = make(map[string]FactorEntry, len(factorsTable.Factors))
	for _, f := range factorsTable.Factors {
		key := strings.ToLower(f.Model) + "|" + strings.ToLower(f.Region)
		factorsIdx[key] = f
	}
}

// LookupCoefficient returns the gCO2e/1k-tokens coefficient + version for
// (model, region). If region is empty, falls back to FactorsTable.DefaultRegion.
// Returns ok=false when the model has no entry — caller should write a
// usage_carbon row with co2g_estimate=NULL and notes='coefficient_gap'.
func LookupCoefficient(model, region string) (gco2e float64, version string, ok bool) {
	factorsOnce.Do(loadFactors)
	if factorsIdx == nil {
		return 0, "", false
	}
	if region == "" {
		region = factorsTable.DefaultRegion
	}
	key := strings.ToLower(model) + "|" + strings.ToLower(region)
	if entry, found := factorsIdx[key]; found {
		return entry.GCO2ePer1KTokens, entry.Version, true
	}
	// Fallback: try the default region for this model
	if region != factorsTable.DefaultRegion {
		key = strings.ToLower(model) + "|" + strings.ToLower(factorsTable.DefaultRegion)
		if entry, found := factorsIdx[key]; found {
			return entry.GCO2ePer1KTokens, entry.Version, true
		}
	}
	return 0, "", false
}

// EstimateCO2 returns the estimated CO2 in grams for a given token count and
// model+region. Returns 0 + ok=false when no coefficient exists.
func EstimateCO2(tokens int, model, region string) (co2g float64, version string, ok bool) {
	coef, ver, ok := LookupCoefficient(model, region)
	if !ok {
		return 0, "", false
	}
	return float64(tokens) / 1000.0 * coef, ver, true
}

// GetFactorsJSON returns the raw embedded JSON bytes for the
// /api/carbon/factors endpoint to pass straight through to the client.
func GetFactorsJSON() []byte {
	factorsOnce.Do(loadFactors)
	return carbonFactorsJSON
}

// GetFactorsTable returns the parsed table (used by tests and internal code).
func GetFactorsTable() *FactorsTable {
	factorsOnce.Do(loadFactors)
	return &factorsTable
}
