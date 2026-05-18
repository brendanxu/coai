package main

import (
	"chat/admin"
	"chat/auth"
	"chat/billing"
	"chat/carbon"
	"chat/channel"
	"chat/cli"
	"chat/commerce"
	"chat/connection"
	"chat/globals"
	"chat/lead"
	"chat/middleware"
	"chat/newapi"
	"chat/payment"
	"chat/plans"
	"chat/relay"
	"chat/service"
	"chat/usage"
	"chat/utils"
	"chat/waitlist"
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"net/url"
	"time"
)

// registerStorageRoute serves /storage/orders/<order_no>/<file> from
// the on-disk volume mounted at /storage (or storage.root override).
// Path traversal is prevented by gin's filepath.Clean on c.File.
func registerStorageRoute(engine *gin.Engine) {
	root := viper.GetString("storage.root")
	if root == "" {
		root = "/storage"
	}
	engine.GET("/storage/orders/:order_no/:file", func(c *gin.Context) {
		orderNo := c.Param("order_no")
		file := c.Param("file")
		// Reject any path containing separators in either segment —
		// gin's :param normally doesn't accept slashes, but defense in
		// depth is cheap.
		if orderNo == "" || file == "" ||
			containsAnyByte(orderNo, "/\\") ||
			containsAnyByte(file, "/\\") {
			c.JSON(400, gin.H{"status": false, "message": "invalid path"})
			return
		}
		c.File(fmt.Sprintf("%s/orders/%s/%s", root, orderNo, file))
	})
}

func containsAnyByte(s, chars string) bool {
	for i := 0; i < len(s); i++ {
		for j := 0; j < len(chars); j++ {
			if s[i] == chars[j] {
				return true
			}
		}
	}
	return false
}

func readCorsOrigins() {
	origins := viper.GetStringSlice("allow_origins")
	if len(origins) > 0 {
		globals.AllowedOrigins = utils.Each(origins, func(origin string) string {
			// remove protocol and trailing slash
			// e.g. https://chatnio.net/ -> chatnio.net

			if host, err := url.Parse(origin); err == nil {
				return host.Host
			}

			return origin
		})
	}
}

func registerApiRouter(engine *gin.Engine) {
	var app *gin.RouterGroup
	if !viper.GetBool("serve_static") {
		app = engine.Group("")
	} else {
		app = engine.Group("/api")
	}

	{
		auth.Register(app)
		admin.Register(app)
		payment.Register(app)
		// v0.6 carbon routes
		carbon.Register(app)
		// v0.6.1 waitlist (marketing landing email capture)
		waitlist.Register(app)
		// v0.9 newapi pool + api-key binding (greentokey 3-layer Layer 1+2)
		newapi.Register(app)
		// v0.9 service catalog + order (greentokey 3-layer Layer 3)
		service.Register(app)
		// v0.10.2 marketing-side lead capture (民宿主 demo 预约)
		lead.Register(app)
		// v0.13 admin-side lead pipeline kanban (/admin/mansu)
		lead.RegisterAdmin(app)
		// v0.16 menu bar app: per-user usage feed (GET /api/v1/usage/me)
		usage.Register(app)
	}
}

func main() {
	utils.ReadConf()
	admin.InitInstance()
	channel.InitManager()

	if cli.Run() {
		return
	}

	app := utils.NewEngine()
	worker := middleware.RegisterMiddleware(app)
	defer worker()

	// greentokey: bridge tables for LemonSqueezy subscription billing (v0.6+).
	// Runs after middleware.RegisterMiddleware connects DB; idempotent on reboot.
	//
	// Order is FK-dependency driven, NOT alphabetical (PKG-1 broke the
	// alphabetical assumption by introducing cross-package FKs; PKG-2 added
	// commerce + a margin VIEW that JOINs gtk_app_usage_log):
	//   payment   creates gtk_ls_subscription      ← gtk_service_order's FK target
	//   service   creates gtk_service              ← gtk_plan's PKG-1 FK target
	//   plans     creates gtk_plan + ALTERs gtk_app_usage_log adding source/order_id/provider
	//                                              ← commerce VIEW depends on order_id
	//   newapi    creates gtk_newapi_pending_provisions ← FK to gtk_plan
	//   commerce  creates gtk_payment_session + gtk_service_margin_v VIEW
	//                                              ← VIEW JOINs gtk_app_usage_log.order_id (plans)
	//                                                AND gtk_service_order (service)
	//   lead / waitlist: independent tables, no cross-package FKs — appended last.
	// Fix history:
	//   2026-05-10 deploy v0.20 panic 1: ENUM helper in service/migration.go
	//     spliced into VARCHAR — fixed by early-return when COLUMN_TYPE != enum(
	//   2026-05-10 deploy v0.20 panic 2: commerce ran BEFORE plans, so VIEW
	//     JOIN on gtk_app_usage_log.order_id failed (column not yet added) —
	//     fixed by moving commerce.Migrate to AFTER plans + newapi
	if err := carbon.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey carbon migration failed: %s", err))
	}
	if err := payment.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey payment migration failed: %s", err))
	}
	if err := service.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey service migration failed: %s", err))
	}
	if err := plans.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey plans migration failed: %s", err))
	}
	if err := newapi.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey newapi migration failed: %s", err))
	}
	if err := commerce.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey commerce migration failed: %s", err))
	}
	// Idempotent catalog seed runs after service.Migrate. Existing
	// rows are never overwritten — operators can edit via SQL or admin
	// UI and their changes win on the next boot.
	if err := service.SeedCatalog(connection.DB); err != nil {
		// Log but don't panic — catalog seed is best-effort. A single
		// bad row shouldn't keep the gateway from booting.
		globals.Warn(fmt.Sprintf("greentokey service seed: %s", err))
	}
	if err := waitlist.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey waitlist migration failed: %s", err))
	}
	if err := lead.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey lead migration failed: %s", err))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := billing.StartCron(ctx, connection.DB); err != nil {
		globals.Warn(fmt.Sprintf("billing: cron start failed: %s", err))
	}
	if !newapi.IsConfigured() {
		// Boot-time visibility: greentokey starts cleanly even if NewAPI
		// integration is intentionally deferred (e.g. dev / test). The
		// payment provisioning hook degrades to "log + skip" rather than
		// fail on a per-purchase basis.
		globals.Warn("newapi: admin_access_token not configured — purchase → key provisioning will no-op")
	}

	// v0.15: serve customer order uploads from the /storage volume.
	// Registered BEFORE RegisterStaticRoute so the catch-all SPA
	// fallback doesn't swallow /storage/* requests.
	registerStorageRoute(app)

	// PKG-3 PKG-TOKEN-PRODUCT-RENTAL: drain pending NewAPI provisions in
	// background. PKG-2's commerce.GrantEntitlement enqueues a row into
	// gtk_newapi_pending_provisions when ProvisionForPlan fails
	// transiently (network blip, NewAPI 5xx, etc.). This goroutine
	// retries with exponential backoff so the user gets their sk-xxx
	// token shortly after the outage clears, without manual ops
	// intervention.
	//
	// Interval matches the bin/check-pending-provisioning.sh cron's
	// 5-minute alerting window — worker pulses every 60s so transient
	// failures usually resolve well before the cron fires the
	// stuck-row alert. Reuses the package-level ctx from billing.StartCron
	// above so a future graceful-shutdown path can cancel both workers
	// together.
	go newapi.DrainPendingProvisionsForever(ctx, connection.DB, 60*time.Second)

	utils.RegisterStaticRoute(app)
	registerApiRouter(app)
	// PKG-A-4: OpenAI-compatible /v1/* relay proxy.
	// Mounts at engine root (NOT under /api group) so aidesk can call
	// POST https://www.greentokey.com/v1/chat/completions directly.
	// NewAPI :3000 remains hidden; this handler is the only bridge.
	relay.Register(app)
	readCorsOrigins()

	if err := app.Run(fmt.Sprintf(":%s", viper.GetString("server.port"))); err != nil {
		panic(err)
	}
}
