package carbon

import "github.com/gin-gonic/gin"

// Register mounts the carbon API on the existing CoAI gin router group.
// Called from main.go:registerApiRouter alongside auth.Register, etc.
//
// Routes (all auth-required):
//   GET  /api/carbon/summary?month=YYYY-MM
//   GET  /api/carbon/by-model?from=YYYY-MM-DD&to=YYYY-MM-DD
//   GET  /api/carbon/factors                       (raw embedded coefficient table)
//   GET  /api/user/eco-mode                        (current prefs)
//   PUT  /api/user/eco-mode                        (toggle)
//   POST /api/user/eco-mode/feedback-seen          (mark first-feedback dismissed)
//
// Note: when serve_static is on, main.go uses /api group; when off, root group.
// Routes here are relative — the parent group prefix already covers /api.
func Register(app *gin.RouterGroup) {
	app.GET("/carbon/summary", summaryHandler)
	app.GET("/carbon/by-model", byModelHandler)
	app.GET("/carbon/factors", factorsHandler)

	app.GET("/user/eco-mode", prefsHandler)
	app.PUT("/user/eco-mode", setEcoModeHandler)
	app.POST("/user/eco-mode/feedback-seen", markFeedbackSeenHandler)
}
