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
	"chat/lead"
	"chat/manager"
	"chat/manager/conversation"
	"chat/middleware"
	"chat/newapi"
	"chat/payment"
	"chat/plans"
	"chat/service"
	"chat/usage"
	"chat/utils"
	"chat/waitlist"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"net/url"
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
	// Order: alphabetical by package name (carbon → lead → newapi → payment → plans → service → waitlist).
	if err := carbon.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey carbon migration failed: %s", err))
	}
	if err := lead.Migrate(connection.DB); err != nil {
		panic(fmt.Sprintf("greentokey lead migration failed: %s", err))
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

	// v0.15: serve customer order uploads from the /storage volume.
	// Registered BEFORE RegisterStaticRoute so the catch-all SPA
	// fallback doesn't swallow /storage/* requests.
	registerStorageRoute(app)
	utils.RegisterStaticRoute(app)
	registerApiRouter(app)
	readCorsOrigins()

	if err := app.Run(fmt.Sprintf(":%s", viper.GetString("server.port"))); err != nil {
		panic(err)
	}
}
