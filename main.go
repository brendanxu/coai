package main

import (
	"chat/adapter"
	"chat/addition"
	"chat/admin"
	"chat/auth"
	"chat/carbon"
	"chat/channel"
	"chat/cli"
	"chat/commerce"
	"chat/connection"
	"chat/globals"
	"chat/manager"
	"chat/manager/conversation"
	"chat/middleware"
	"chat/newapi"
	"chat/payment"
	"chat/plans"
	"chat/service"
	"chat/utils"
	"chat/waitlist"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"net/url"
)

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
		adapter.Register(app)
		manager.Register(app)
		addition.Register(app)
		conversation.Register(app)
		payment.Register(app)
		// v0.6 carbon routes
		carbon.Register(app)
		// v0.6.1 waitlist (marketing landing email capture)
		waitlist.Register(app)
		// v0.9 newapi pool + api-key binding (greentokey 3-layer Layer 1+2)
		newapi.Register(app)
		// v0.9 service catalog + order (greentokey 3-layer Layer 3)
		service.Register(app)
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
	// commerce):
	//   payment   creates gtk_ls_subscription      ← gtk_service_order's FK target
	//   service   creates gtk_service              ← gtk_plan's PKG-1 FK target
	//   commerce  creates gtk_payment_session      (FK only to auth — no order
	//                                                dependency, but logically
	//                                                grouped with payment)
	//   plans     creates gtk_plan                 ← gtk_newapi_pending_provisions's PKG-1 FK target
	//   newapi    creates gtk_newapi_pending_provisions + gtk_newapi_binding
	// Fresh MySQL boot would PANIC if newapi runs before plans (or plans before
	// service) because InnoDB rejects FOREIGN KEY pointing at a non-existent
	// table at CREATE TABLE / ADD CONSTRAINT time. SQLite is permissive and
	// would silently succeed, hiding the bug — that's how PKG-1 tests passed.
	if err := carbon.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey carbon migration failed: %s", err))
	}
	if err := payment.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey payment migration failed: %s", err))
	}
	if err := service.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey service migration failed: %s", err))
	}
	if err := commerce.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey commerce migration failed: %s", err))
	}
	if err := plans.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey plans migration failed: %s", err))
	}
	if err := newapi.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey newapi migration failed: %s", err))
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
	if !newapi.IsConfigured() {
		// Boot-time visibility: greentokey starts cleanly even if NewAPI
		// integration is intentionally deferred (e.g. dev / test). The
		// payment provisioning hook degrades to "log + skip" rather than
		// fail on a per-purchase basis.
		globals.Warn("newapi: admin_access_token not configured — purchase → key provisioning will no-op")
	}

	utils.RegisterStaticRoute(app)
	registerApiRouter(app)
	readCorsOrigins()

	if err := app.Run(fmt.Sprintf(":%s", viper.GetString("server.port"))); err != nil {
		panic(err)
	}
}
