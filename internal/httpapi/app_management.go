package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/appmanagement"
	"github.com/mingzaily/mailwake/internal/coreidentity"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/storage"
)

const appPrefix = "/api/v1/app"

func credentialHash(credential string) string {
	sum := sha256.Sum256([]byte(credential))
	return hex.EncodeToString(sum[:])
}

func appManagementRoutes(r *gin.Engine, admin *gin.RouterGroup, runtime Runtime, store *storage.Store, service *appmanagement.Service) {
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

	app := appRouteRegistrar{group: r.Group(appPrefix), store: store}
	app.HandleController("GET", "/controller", "", func(c *gin.Context, controller storage.AppController) { c.JSON(200, viewController(controller)) })
	app.HandleController("DELETE", "/controller", "", func(c *gin.Context, controller storage.AppController) {
		if err := store.DeleteAppController(c.Request.Context(), controller.ID); err != nil {
			respondFault(c, err)
			return
		}
		c.Status(204)
	})
	app.Handle("POST", "/content", "content", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		var input struct {
			Reference coreidentity.MessageReference `json:"reference"`
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
	app.Handle("POST", "/content/read", "content", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		var input struct {
			Reference coreidentity.MessageReference `json:"reference"`
		}
		if !decodeRequest(c, &input) {
			return
		}
		if err := runtime.MarkRead(c.Request.Context(), input.Reference); err != nil {
			respondFault(c, err)
			return
		}
		c.Status(204)
	})
	app.Handle("GET", "/folder-mailboxes", "folders", func(c *gin.Context) {
		items := []gin.H{}
		for _, mailbox := range runtime.Mailboxes() {
			items = append(items, gin.H{"id": mailbox.ID, "label": mailbox.Label,
				"revision": mailbox.Revision, "connection_limit": mailbox.ConnectionLimit})
		}
		c.JSON(200, gin.H{"mailboxes": items})
	})
	app.Handle("GET", "/native/devices", "channels", func(c *gin.Context) {
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
	mailboxes := mailboxHandlers{runtime: runtime}
	app.Handle("GET", "/mailboxes", "mailboxes", mailboxes.List)
	app.HandleController("POST", "/mailboxes", "mailboxes", func(c *gin.Context, controller storage.AppController) {
		mailboxes.create(c, controller.HasScope("folders"))
	})
	app.HandleController("POST", "/mailboxes/scan", "mailboxes", func(c *gin.Context, controller storage.AppController) {
		if !controller.HasScope("folders") {
			respondError(c, 403, fault.New("forbidden"))
			return
		}
		mailboxTestHandler(runtime, func(c *gin.Context, folders *mail.FolderDiscovery) { c.JSON(200, folders) })(c)
	})
	app.Handle("GET", "/mailboxes/:id", "mailboxes", mailboxes.Get)
	app.Handle("PUT", "/mailboxes/:id", "mailboxes", mailboxes.Update)
	app.Handle("DELETE", "/mailboxes/:id", "mailboxes", mailboxes.Delete)
	app.Handle("GET", "/mailboxes/:id/folders", "folders", mailboxes.Folders)
	app.Handle("GET", "/mailboxes/:id/subscriptions", "folders", mailboxes.Subscriptions)
	app.Handle("PUT", "/mailboxes/:id/subscriptions", "folders", mailboxes.UpdateSubscriptions)
	testMailbox := mailboxTestHandler(runtime, func(c *gin.Context, _ *mail.FolderDiscovery) { c.JSON(200, gin.H{"status": "connected"}) })
	app.Handle("POST", "/mailboxes/test", "mailboxes", testMailbox)
	app.Handle("POST", "/mailboxes/:id/test", "mailboxes", testMailbox)
	delivery := deliveryHandlers{runtime: runtime}
	app.Handle("GET", "/settings/delivery", "channels", delivery.Get)
	app.Handle("PUT", "/settings/delivery", "channels", delivery.Update)
	app.Handle("POST", "/settings/delivery/test", "channels", delivery.Test)
	app.Handle("DELETE", "/settings/delivery", "channels", delivery.Delete)
	app.Handle("GET", "/diagnostics", "diagnostics", diagnosticsHandler(runtime, store))
	app.Handle("GET", "/status", "diagnostics", statusHandler(runtime, store))
	app.Handle("GET", "/deliveries", "diagnostics", deliveriesHandler(store))

	limiter := &savedTestLimiter{last: map[string]time.Time{}}
	app.HandleController("POST", "/mailboxes/:id/test-saved", "mailboxes", func(c *gin.Context, controller storage.AppController) {
		if !limiter.allow(c, controller.ID, "mailbox:"+c.Param("id")) {
			return
		}
		if err := runtime.TestSavedMailbox(c.Request.Context(), c.Param("id")); err != nil {
			respondFault(c, err)
			return
		}
		c.JSON(200, gin.H{"status": "connected"})
	})
	app.HandleController("POST", "/settings/delivery/test-saved", "channels", func(c *gin.Context, controller storage.AppController) {
		if !limiter.allow(c, controller.ID, "delivery") {
			return
		}
		if err := runtime.TestSavedDelivery(c.Request.Context()); err != nil {
			respondFault(c, err)
			return
		}
		c.Status(204)
	})
}

// appRouteRegistrar binds the route, owner-approved scope and handler together.
// Controller-aware handlers receive the identity authenticated for this request.
type appRouteRegistrar struct {
	group *gin.RouterGroup
	store *storage.Store
}

func (r appRouteRegistrar) Handle(method, path, scope string, handler gin.HandlerFunc) {
	r.HandleController(method, path, scope, func(c *gin.Context, _ storage.AppController) { handler(c) })
}
func (r appRouteRegistrar) HandleController(method, path, scope string, handler func(*gin.Context, storage.AppController)) {
	r.group.Handle(method, path, func(c *gin.Context) {
		credential, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok || !strings.HasPrefix(credential, "mwac_") {
			respondError(c, 401, fault.New("unauthorized"))
			return
		}
		controller, err := r.store.AuthenticateAppController(c.Request.Context(), credentialHash(credential))
		if err != nil {
			respondFault(c, err)
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
		handler(c, controller)
	})
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

// savedTestLimiter allows one saved-configuration test per controller and target a minute.
type savedTestLimiter struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (l *savedTestLimiter) allow(c *gin.Context, controllerID, target string) bool {
	key := controllerID + "|" + target
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
