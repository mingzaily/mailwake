package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/storage"
)

// Device-bound ciphertext uses pairing authorization, independently of content and management scopes.
func appNativeNotificationRoutes(app appRouteRegistrar) {
	app.HandleController("GET", "/native/notification-preferences", "", func(c *gin.Context, controller storage.AppController) {
		c.Header("Cache-Control", "no-store")
		omit, err := app.store.ActivityNotificationPreference(c.Request.Context(), controller.ID)
		if err != nil {
			databaseError(c)
			return
		}
		c.JSON(200, gin.H{"omit_activity_notification": omit})
	})
	app.HandleController("PUT", "/native/notification-preferences", "", func(c *gin.Context, controller storage.AppController) {
		c.Header("Cache-Control", "no-store")
		var input struct {
			Omit *bool `json:"omit_activity_notification"`
		}
		if !decodeRequest(c, &input) {
			return
		}
		if input.Omit == nil {
			respondFault(c, fault.New("request_invalid"))
			return
		}
		if err := app.store.SetActivityNotificationPreference(c.Request.Context(), controller.ID, *input.Omit); err != nil {
			databaseError(c)
			return
		}
		c.JSON(200, gin.H{"omit_activity_notification": *input.Omit})
	})
	app.HandleController("GET", "/native/notifications", "", func(c *gin.Context, controller storage.AppController) {
		c.Header("Cache-Control", "no-store")
		items, err := app.store.PendingNativeNotifications(c.Request.Context(), controller.DeviceID)
		if err != nil {
			databaseError(c)
			return
		}
		c.JSON(200, gin.H{"items": items})
	})
	app.HandleController("DELETE", "/native/notifications/:pairing_id/:event_id", "", func(c *gin.Context, controller storage.AppController) {
		c.Header("Cache-Control", "no-store")
		if err := app.store.AcknowledgeNativeNotification(c.Request.Context(), controller.DeviceID, c.Param("pairing_id"), c.Param("event_id")); err != nil {
			databaseError(c)
			return
		}
		c.Status(204)
	})
}
