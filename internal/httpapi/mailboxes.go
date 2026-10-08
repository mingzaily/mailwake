package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/settings"
)

func mailboxRoutes(api *gin.RouterGroup, runtime Runtime) {
	api.GET("/mailboxes", func(c *gin.Context) { c.JSON(200, gin.H{"mailboxes": runtime.Mailboxes()}) })
	api.POST("/mailboxes", func(c *gin.Context) {
		var input settings.MailboxUpdate
		if !decodeRequest(c, &input) {
			return
		}
		if input.Revision != 0 {
			respondError(c, 400, fault.New("request_invalid"))
			return
		}
		view, err := runtime.CreateMailbox(c.Request.Context(), input)
		if err != nil {
			respondFault(c, err)
			return
		}
		c.JSON(201, view)
	})
	api.GET("/mailboxes/:id", func(c *gin.Context) {
		view, err := runtime.Mailbox(c.Param("id"))
		if err != nil {
			respondFault(c, err)
			return
		}
		c.JSON(200, view)
	})
	api.PUT("/mailboxes/:id", func(c *gin.Context) {
		var input settings.MailboxUpdate
		if !decodeRequest(c, &input) {
			return
		}
		if input.Revision < 1 {
			respondError(c, 400, fault.New("request_invalid"))
			return
		}
		if err := runtime.UpdateMailbox(c.Request.Context(), c.Param("id"), input); err != nil {
			respondFault(c, err)
			return
		}
		view, err := runtime.Mailbox(c.Param("id"))
		if err != nil {
			respondFault(c, err)
			return
		}
		c.JSON(200, view)
	})
	api.DELETE("/mailboxes/:id", func(c *gin.Context) {
		if err := runtime.DeleteMailbox(c.Request.Context(), c.Param("id")); err != nil {
			respondFault(c, err)
			return
		}
		c.Status(204)
	})
	test := func(c *gin.Context) {
		var input settings.MailboxUpdate
		if !decodeRequest(c, &input) {
			return
		}
		folders, err := runtime.TestMailbox(c.Request.Context(), c.Param("id"), input)
		if err != nil {
			respondFault(c, err)
			return
		}
		if _, app := c.Get("app_controller"); app {
			c.JSON(200, gin.H{"status": "connected"})
			return
		}
		c.JSON(200, gin.H{"folders": folders})
	}
	api.POST("/mailboxes/test", test)
	api.POST("/mailboxes/:id/test", test)
	api.GET("/mailboxes/:id/folders", func(c *gin.Context) {
		folders, err := runtime.Folders(c.Request.Context(), c.Param("id"))
		if err != nil {
			failure := fault.From(err, "imap_discovery_failed")
			switch failure.Code {
			case "mailbox_not_found", "mailbox_required", "connection_budget_exceeded", "discovery_busy":
				respondFault(c, failure)
			default:
				respondError(c, 502, failure)
			}
			return
		}
		c.JSON(200, gin.H{"folders": folders})
	})
	api.GET("/mailboxes/:id/subscriptions", func(c *gin.Context) {
		state, err := runtime.Subscriptions(c.Param("id"))
		if err != nil {
			respondFault(c, err)
			return
		}
		c.JSON(200, state)
	})
	api.PUT("/mailboxes/:id/subscriptions", func(c *gin.Context) {
		var input struct {
			Revision *int64         `json:"revision"`
			Folders  *[]mail.Folder `json:"folders"`
		}
		if !decodeRequest(c, &input) {
			return
		}
		if input.Revision == nil || *input.Revision < 1 || input.Folders == nil {
			respondError(c, 400, fault.New("subscriptions_request_invalid"))
			return
		}
		state, err := runtime.UpdateSubscriptions(c.Request.Context(), c.Param("id"), mail.Subscriptions{Revision: *input.Revision, Folders: *input.Folders})
		if err != nil {
			respondFault(c, err)
			return
		}
		c.JSON(202, state)
	})
}
