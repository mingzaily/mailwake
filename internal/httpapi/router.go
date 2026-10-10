package httpapi

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/auth"
	"github.com/mingzaily/mailwake/internal/engine"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/i18n"
	"github.com/mingzaily/mailwake/internal/storage"
)

//go:embed all:webdist
var assets embed.FS

//go:embed placeholder.html
var placeholder []byte

func New(authService *auth.Service, runtime Runtime, store *storage.Store, log *slog.Logger) http.Handler {
	r := gin.New()
	_ = r.SetTrustedProxies([]string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"})
	r.HandleMethodNotAllowed = true
	r.Use(func(c *gin.Context) {
		locale := i18n.Match(c.GetHeader("Accept-Language"))
		c.Set("language", locale)
		c.Header("Content-Language", locale)
		c.Header("Vary", "Accept-Language")
		defer func() {
			if recover() != nil {
				log.Error(i18n.Message("en", "log.http_panic", nil))
				respondError(c, 500, fault.New("request_failed"))
			}
		}()
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'; font-src 'self'; img-src 'self' data:")

		publicSetup := c.Request.Method == "POST" && c.Request.URL.Path == "/api/v1/setup"
		publicCapabilities := c.Request.Method == "GET" && c.Request.URL.Path == "/api/v1/app/capabilities"
		if strings.HasPrefix(c.Request.URL.Path, "/api/v1/") && !publicSetup && !publicCapabilities {
			ready, err := authService.Ready(c.Request.Context())
			if err != nil {
				databaseError(c)
				return
			}
			if !ready {
				respondError(c, 403, fault.New("setup_required"))
				return
			}
		}
		c.Next()
	})
	r.GET("/", func(c *gin.Context) {
		data, err := managementIndex(assets)
		if err != nil {
			respondError(c, 500, fault.New("request_failed"))
			return
		}
		c.Data(200, "text/html; charset=utf-8", data)
	})
	r.GET("/assets/*file", func(c *gin.Context) {
		name := strings.TrimPrefix(c.Param("file"), "/")
		if name == "" || path.Clean(name) != name || strings.Contains(name, "/") {
			respondError(c, 404, fault.New("route_not_found"))
			return
		}
		data, err := assets.ReadFile("webdist/assets/" + name)
		if err != nil {
			respondError(c, 404, fault.New("route_not_found"))
			return
		}
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		c.Data(200, mime.TypeByExtension(path.Ext(name)), data)
	})
	r.GET("/third-party-notices.txt", func(c *gin.Context) {
		data, err := assets.ReadFile("webdist/third-party-notices.txt")
		if err != nil {
			respondError(c, 404, fault.New("route_not_found"))
			return
		}
		c.Data(200, "text/plain; charset=utf-8", data)
	})
	r.GET("/locales/:file", func(c *gin.Context) {
		locale := strings.TrimSuffix(c.Param("file"), ".json")
		if locale == "default" {
			locale = c.GetString("language")
		}
		if !strings.HasSuffix(c.Param("file"), ".json") || !i18n.Supported(locale) {
			respondError(c, 404, fault.New("locale_not_supported"))
			return
		}
		data, err := i18n.Catalog(locale)
		if err != nil {
			respondError(c, 500, fault.New("request_failed"))
			return
		}
		c.Header("Content-Language", locale)
		c.Data(200, "application/json; charset=utf-8", data)
	})
	r.NoRoute(func(c *gin.Context) { respondError(c, 404, fault.New("route_not_found")) })
	r.NoMethod(func(c *gin.Context) { respondError(c, 405, fault.New("method_not_allowed")) })
	r.GET("/healthz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := store.Ping(ctx); err != nil {
			c.JSON(503, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(200, gin.H{"status": "ok"})
	})
	setupRoute(r, authService)
	loginRoute(r, authService)
	api := r.Group("/api/v1", authenticate(authService))
	logsRoute(api, log)
	sessionRoutes(api, authService)
	tokenRoutes(api, authService)
	settingsRoutes(api, runtime)
	nativeRoutes(api, runtime)
	appManagementRoutes(r, api, runtime, store)
	mailboxRoutes(api, runtime)
	managementRoutes(api, runtime, store)
	api.GET("/status", statusHandler(runtime, store))
	api.GET("/deliveries", deliveriesHandler(store))
	api.DELETE("/deliveries", func(c *gin.Context) {
		deleted, err := store.ClearDeliveryHistory(c.Request.Context())
		if err != nil {
			databaseError(c)
			return
		}
		c.JSON(200, gin.H{"deleted": deleted})
	})
	api.POST("/test-push", func(c *gin.Context) {
		if err := runtime.TestSavedDelivery(c.Request.Context()); err != nil {
			respondFault(c, err)
			return
		}
		c.Status(204)
	})
	api.POST("/deliveries/:id/retry", func(c *gin.Context) {
		ok, err := store.Retry(c.Request.Context(), c.Param("id"))
		if err != nil {
			databaseError(c)
			return
		}
		if !ok {
			respondError(c, 409, fault.New("delivery_retry_conflict"))
			return
		}
		c.JSON(202, gin.H{"state": "pending"})
	})
	return r
}

type localizedError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Params  map[string]string `json:"params,omitempty"`
}
type folderView struct {
	engine.FolderStatus
	LastError *localizedError `json:"last_error,omitempty"`
	Notice    *localizedError `json:"notice,omitempty"`
}
type deliveryView struct {
	Message    *storage.DeliveryMessage `json:"message,omitempty"`
	Channel    string                   `json:"channel,omitempty"`
	Devices    []storage.NativeDelivery `json:"devices,omitempty"`
	ID         string                   `json:"id"`
	State      string                   `json:"state"`
	Attempts   int                      `json:"attempts"`
	CreatedAt  time.Time                `json:"created_at"`
	AcceptedAt *time.Time               `json:"accepted_at,omitempty"`
	LastError  *localizedError          `json:"last_error,omitempty"`
}

func localize(c *gin.Context, failure *fault.Error) *localizedError {
	if failure == nil {
		return nil
	}
	return &localizedError{Code: failure.Code, Message: i18n.Message(c.GetString("language"), failure.Code, failure.Params), Params: failure.Params}
}
func respondError(c *gin.Context, status int, failure *fault.Error) {
	c.AbortWithStatusJSON(status, gin.H{"error": localize(c, failure)})
}
func databaseError(c *gin.Context) { respondError(c, 503, fault.New("database_unavailable")) }

// optionalTime converts a nullable database timestamp using its storage unit.
func optionalTime(value *int64, convert func(int64) time.Time) *time.Time {
	if value == nil {
		return nil
	}
	converted := convert(*value).UTC()
	return &converted
}

func managementIndex(files fs.FS) ([]byte, error) {
	data, err := fs.ReadFile(files, "webdist/index.html")
	if errors.Is(err, fs.ErrNotExist) {
		return placeholder, nil
	}
	return data, err
}

func statusHandler(runtime Runtime, store *storage.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		summary, err := store.Summary(c.Request.Context())
		if err != nil {
			databaseError(c)
			return
		}
		folders := make([]folderView, 0)
		for _, status := range runtime.Status() {
			item := folderView{FolderStatus: status, LastError: localize(c, status.LastError)}
			if status.Notice != "" {
				item.Notice = localize(c, fault.New(status.Notice))
			}
			folders = append(folders, item)
		}
		notices := make(map[string]*localizedError)
		for scope, notice := range runtime.Notices() {
			notices[scope] = localize(c, notice)
		}
		if runtime.Channel() == "native" && notices["delivery"] == nil {
			devices, err := store.NativePairings(c.Request.Context(), "active")
			if err != nil {
				databaseError(c)
				return
			}
			target, _ := runtime.DeliveryView()["native_pairing_id"].(string)
			found := false
			for _, device := range devices {
				if device.ID == target {
					found = true
					break
				}
			}
			if !found {
				notices["delivery"] = localize(c, fault.New("native_target_unavailable"))
			}
		}
		c.JSON(200, gin.H{"folders": folders, "delivery": summary, "channel": runtime.Channel(), "notices": notices})
	}
}

func deliveriesHandler(store *storage.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := store.Recent(c.Request.Context())
		if err != nil {
			databaseError(c)
			return
		}
		nativeItems, err := store.RecentNativeDeliveries(c.Request.Context())
		if err != nil {
			databaseError(c)
			return
		}
		byEvent := map[string][]storage.NativeDelivery{}
		for _, item := range nativeItems {
			byEvent[item.EventID] = append(byEvent[item.EventID], item)
		}
		records := make([]deliveryView, 0, len(items))
		for _, item := range items {
			records = append(records, deliveryView{Message: item.Message, Channel: item.Channel, Devices: byEvent[item.ID], ID: item.ID, State: item.State, Attempts: item.Attempts, CreatedAt: time.UnixMilli(item.CreatedAt).UTC(), AcceptedAt: optionalTime(item.AcceptedAt, time.UnixMilli), LastError: localize(c, item.LastError)})
		}
		c.JSON(200, gin.H{"deliveries": records})
	}
}
