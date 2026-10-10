package httpapi

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/engine"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/native"
	"github.com/mingzaily/mailwake/internal/settings"
)

// Runtime is the mailbox-aware application boundary for HTTP management.
type Runtime interface {
	ReadContent(context.Context, native.MessageReference) (mail.Body, error)
	Status() []engine.FolderStatus
	Mailboxes() []map[string]any
	Mailbox(string) (map[string]any, error)
	CreateMailbox(context.Context, settings.MailboxUpdate) (map[string]any, error)
	UpdateMailbox(context.Context, string, settings.MailboxUpdate) error
	DeleteMailbox(context.Context, string) error
	TestMailbox(context.Context, string, settings.MailboxUpdate) (*mail.FolderDiscovery, error)
	TestSavedMailbox(context.Context, string) error
	Folders(context.Context, string) (*mail.FolderDiscovery, error)
	Subscriptions(string) (mail.Subscriptions, error)
	UpdateSubscriptions(context.Context, string, mail.Subscriptions) (mail.Subscriptions, error)
	Channel() string
	Notices() map[string]*fault.Error
	DeliveryView() map[string]any
	UpdateDelivery(context.Context, settings.DeliveryUpdate) error
	DeleteDelivery(context.Context, int64) error
	TestDelivery(context.Context, settings.DeliveryUpdate) error
	TestSavedDelivery(context.Context) error
}

func settingsRoutes(api *gin.RouterGroup, s Runtime) {
	api.GET("/settings/delivery", func(c *gin.Context) { c.JSON(200, s.DeliveryView()) })
	api.PUT("/settings/delivery", func(c *gin.Context) {
		var body settings.DeliveryUpdate
		if !decodeRequest(c, &body) {
			return
		}
		if body.Revision == nil || *body.Revision < 0 {
			respondError(c, 400, fault.New("request_invalid"))
			return
		}
		if err := s.UpdateDelivery(c.Request.Context(), body); err != nil {
			respondFault(c, err)
			return
		}
		c.JSON(200, s.DeliveryView())
	})
	api.POST("/settings/delivery/test", func(c *gin.Context) {
		var input settings.DeliveryUpdate
		if !decodeRequest(c, &input) {
			return
		}
		if err := s.TestDelivery(c.Request.Context(), input); err != nil {
			respondFault(c, err)
			return
		}
		c.Status(204)
	})
	api.DELETE("/settings/delivery", func(c *gin.Context) {
		var input struct {
			Revision *int64 `json:"revision"`
		}
		if !decodeRequest(c, &input) {
			return
		}
		if input.Revision == nil || *input.Revision < 0 {
			respondError(c, 400, fault.New("request_invalid"))
			return
		}
		if err := s.DeleteDelivery(c.Request.Context(), *input.Revision); err != nil {
			respondFault(c, err)
			return
		}
		c.Status(204)
	})
}
