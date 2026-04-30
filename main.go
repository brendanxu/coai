package main

import (
	"chat/adapter"
	"chat/addition"
	"chat/admin"
	"chat/auth"
	"chat/carbon"
	"chat/channel"
	"chat/cli"
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
	// Order: alphabetical by package name (carbon → newapi → payment → plans → service → waitlist).
	if err := carbon.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey carbon migration failed: %s", err))
	}
	if err := newapi.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey newapi migration failed: %s", err))
	}
	if err := payment.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey payment migration failed: %s", err))
	}
	if err := plans.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey plans migration failed: %s", err))
	}
	if err := service.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey service migration failed: %s", err))
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
