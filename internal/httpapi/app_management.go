package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/appmanagement"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/native"
	"github.com/mingzaily/mailwake/internal/storage"
)

// App routes map directly to the owner-approved scope they require.
var appRoutes = map[string]string{
	"GET /folder-mailboxes":              "folders",
	"GET /native/devices":                "channels",
	"POST /content":                      "content",
	"GET /mailboxes":                     "mailboxes",
	"POST /mailboxes":                    "mailboxes",
	"POST /mailboxes/test":               "mailboxes",
	"GET /mailboxes/:id":                 "mailboxes",
	"PUT /mailboxes/:id":                 "mailboxes",
	"DELETE /mailboxes/:id":              "mailboxes",
	"POST /mailboxes/:id/test":           "mailboxes",
	"POST /mailboxes/:id/test-saved":     "mailboxes",
	"GET /mailboxes/:id/folders":         "folders",
	"GET /mailboxes/:id/subscriptions":   "folders",
	"PUT /mailboxes/:id/subscriptions":   "folders",
	"GET /settings/delivery":             "channels",
	"PUT /settings/delivery":             "channels",
	"DELETE /settings/delivery":          "channels",
	"POST /settings/delivery/test":       "channels",
	"POST /settings/delivery/test-saved": "channels",
	"GET /status":                        "diagnostics",
	"GET /diagnostics":                   "diagnostics",
	"GET /deliveries":                    "diagnostics",
	"GET /controller":                    "",
	"DELETE /controller":                 "",
}

const appPrefix = "/api/v1/app"

func credentialHash(credential string) string {
	sum := sha256.Sum256([]byte(credential))
	return hex.EncodeToString(sum[:])
}

func appManagementRoutes(r *gin.Engine, admin *gin.RouterGroup, runtime Runtime, store *storage.Store) {
	var service *appmanagement.Service
	if provider, ok := runtime.(interface{ AppManagement() *appmanagement.Service }); ok {
		service = provider.AppManagement()
	}
	if service == nil {
		return
	}

	admin.POST("/device-invitations", func(c *gin.Context) {
		var input appmanagement.InvitationInput
		if !decodeRequest(c, &input) {
			return
		}
		invitation, err := service.Create(c.Request.Context(), input)
		if err != nil {
			nativeError(c, err)
			return
		}
		c.JSON(201, invitation)
	})
	admin.GET("/device-invitations/:id", func(c *gin.Context) {
		status, err := service.InvitationStatus(c.Request.Context(), c.Param("id"))
		if err != nil {
			nativeError(c, err)
			return
		}
		c.JSON(200, status)
	})
	admin.DELETE("/device-invitations/:id", func(c *gin.Context) {
		if err := service.Cancel(c.Request.Context(), c.Param("id")); err != nil {
			nativeError(c, err)
			return
		}
		c.Status(204)
	})
	admin.GET("/management/devices", func(c *gin.Context) {
		controllers, err := store.AppControllers(c.Request.Context())
		if err != nil {
			databaseError(c)
			return
		}
		views := make([]controllerView, 0, len(controllers))
		for _, controller := range controllers {
			views = append(views, viewController(controller))
		}
		c.JSON(200, gin.H{"devices": views})
	})
	admin.DELETE("/management/devices/:id", func(c *gin.Context) {
		if err := store.DeleteAppController(c.Request.Context(), c.Param("id")); err != nil {
			respondFault(c, err)
			return
		}
		c.Status(204)
	})

	r.POST(appPrefix+"/accept", func(c *gin.Context) {
		var input appmanagement.AcceptInput
		if !decodeRequest(c, &input) {
			return
		}
		credential, err := service.Accept(c.Request.Context(), input)
		if err != nil {
			respondFault(c, err)
			return
		}
		c.JSON(200, credential)
	})

	app := r.Group(appPrefix, appAuthenticate(store))
	app.GET("/controller", func(c *gin.Context) { c.JSON(200, viewController(currentController(c))) })
	app.DELETE("/controller", func(c *gin.Context) {
		if err := store.DeleteAppController(c.Request.Context(), currentController(c).ID); err != nil {
			respondFault(c, err)
			return
		}
		c.Status(204)
	})
	app.POST("/content", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		var input struct {
			Reference native.MessageReference `json:"reference"`
		}
		if !decodeRequest(c, &input) {
			return
		}
		result, err := runtime.ReadContent(c.Request.Context(), input.Reference)
		if err != nil {
			respondFault(c, err)
			return
		}
		c.JSON(200, result)
	})
	app.GET("/folder-mailboxes", func(c *gin.Context) {
		items := []gin.H{}
		for _, mailbox := range runtime.Mailboxes() {
			items = append(items, gin.H{"id": mailbox["id"], "label": mailbox["label"],
				"revision": mailbox["revision"], "connection_limit": mailbox["connection_limit"]})
		}
		c.JSON(200, gin.H{"mailboxes": items})
	})
	app.GET("/native/devices", func(c *gin.Context) {
		devices, err := store.NativeDevices(c.Request.Context())
		if err != nil {
			databaseError(c)
			return
		}
		items := []gin.H{}
		for _, device := range devices {
			if device.State == "active" {
				items = append(items, gin.H{"id": device.ID, "device_name": device.DeviceName, "device_id": device.DeviceID})
			}
		}
		c.JSON(200, gin.H{"devices": items})
	})
	mailboxRoutes(app, runtime)
	settingsRoutes(app, runtime)
	managementRoutes(app, runtime, store)
	app.GET("/status", statusHandler(runtime, store))
	app.GET("/deliveries", deliveriesHandler(store))

	limiter := &savedTestLimiter{last: map[string]time.Time{}}
	app.POST("/mailboxes/:id/test-saved", func(c *gin.Context) {
		if !limiter.allow(c, "mailbox:"+c.Param("id")) {
			return
		}
		if err := runtime.TestSavedMailbox(c.Request.Context(), c.Param("id")); err != nil {
			respondFault(c, err)
			return
		}
		c.JSON(200, gin.H{"status": "connected"})
	})
	app.POST("/settings/delivery/test-saved", func(c *gin.Context) {
		if !limiter.allow(c, "delivery") {
			return
		}
		if err := runtime.TestSavedDelivery(c.Request.Context()); err != nil {
			respondFault(c, err)
			return
		}
		c.Status(204)
	})
}

// appAuthenticate resolves the controller credential, then enforces the route's
// owner-approved scope.
func appAuthenticate(store *storage.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		credential, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok || !strings.HasPrefix(credential, "mwac_") {
			respondError(c, 401, fault.New("unauthorized"))
			return
		}
		controller, err := store.AuthenticateAppController(c.Request.Context(), credentialHash(credential))
		if err != nil {
			respondFault(c, err)
			return
		}
		scope, known := appRoutes[c.Request.Method+" "+strings.TrimPrefix(c.FullPath(), appPrefix)]
		if !known {
			respondError(c, 404, fault.New("route_not_found"))
			return
		}
		if scope != "" && !controller.HasScope(scope) {
			code := "forbidden"
			if scope == "content" {
				code = "content_permission_required"
			}
			respondError(c, 403, fault.New(code))
			return
		}
		c.Set("app_controller", controller)
		c.Next()
	}
}

type controllerView struct {
	ID         string     `json:"controller_id"`
	DeviceID   string     `json:"device_id"`
	DeviceName string     `json:"device_name"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func viewController(c storage.AppController) controllerView {
	view := controllerView{ID: c.ID, DeviceID: c.DeviceID, DeviceName: c.DeviceName, Scopes: c.Scopes, CreatedAt: time.Unix(c.CreatedAt, 0).UTC()}
	if c.LastUsedAt > 0 {
		used := time.Unix(c.LastUsedAt, 0).UTC()
		view.LastUsedAt = &used
	}
	return view
}

func currentController(c *gin.Context) storage.AppController {
	return c.MustGet("app_controller").(storage.AppController)
}

// savedTestLimiter allows one saved-configuration test per controller and target a minute.
type savedTestLimiter struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (l *savedTestLimiter) allow(c *gin.Context, target string) bool {
	key := currentController(c).ID + "|" + target
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if previous, ok := l.last[key]; ok && now.Sub(previous) < time.Minute {
		c.Header("Retry-After", "60")
		respondError(c, 429, fault.New("rate_limited"))
		return false
	}
	for k, at := range l.last {
		if now.Sub(at) >= time.Minute {
			delete(l.last, k)
		}
	}
	l.last[key] = now
	return true
}
