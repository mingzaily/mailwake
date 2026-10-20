package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/settings"
)

type mailboxHandlers struct {
	runtime              MailboxRuntime
	includeFolderDisplay bool
}

func (h mailboxHandlers) List(c *gin.Context) {
	items := h.runtime.Mailboxes()
	if !h.includeFolderDisplay {
		for i := range items {
			items[i].FolderDisplay = nil
		}
	}
	c.JSON(200, gin.H{"mailboxes": items})
}

func (h mailboxHandlers) Create(c *gin.Context) { h.create(c, true) }

func (h mailboxHandlers) create(c *gin.Context, allowFolders bool) {
	var input settings.MailboxCreate
	if !decodeRequest(c, &input) {
		return
	}
	if len(input.Folders) > 0 && !allowFolders {
		respondError(c, 403, fault.New("forbidden"))
		return
	}
	if input.Revision != 0 {
		respondError(c, 400, fault.New("request_invalid"))
		return
	}
	view, err := h.runtime.CreateMailbox(c.Request.Context(), input)
	if err != nil {
		respondFault(c, err)
		return
	}
	if !h.includeFolderDisplay {
		view.FolderDisplay = nil
	}
	c.JSON(201, view)
}

func (h mailboxHandlers) Get(c *gin.Context) {
	view, err := h.runtime.Mailbox(c.Param("id"))
	if err != nil {
		respondFault(c, err)
		return
	}
	if !h.includeFolderDisplay {
		view.FolderDisplay = nil
	}
	c.JSON(200, view)
}

func (h mailboxHandlers) Update(c *gin.Context) {
	var input settings.MailboxUpdate
	if !decodeRequest(c, &input) {
		return
	}
	if input.Revision < 1 {
		respondError(c, 400, fault.New("request_invalid"))
		return
	}
	if err := h.runtime.UpdateMailbox(c.Request.Context(), c.Param("id"), input); err != nil {
		respondFault(c, err)
		return
	}
	view, err := h.runtime.Mailbox(c.Param("id"))
	if err != nil {
		respondFault(c, err)
		return
	}
	if !h.includeFolderDisplay {
		view.FolderDisplay = nil
	}
	c.JSON(200, view)
}

func (h mailboxHandlers) Delete(c *gin.Context) {
	if err := h.runtime.DeleteMailbox(c.Request.Context(), c.Param("id")); err != nil {
		respondFault(c, err)
		return
	}
	c.Status(204)
}

func (h mailboxHandlers) Folders(c *gin.Context) {
	folders, err := h.runtime.Folders(c.Request.Context(), c.Param("id"))
	if err != nil {
		failure := fault.From(err, "imap_discovery_failed")
		switch failure.Code {
		case "mailbox_not_found", "mailbox_required", "connection_budget_exceeded", "discovery_busy", "settings_conflict", "database_unavailable":
			respondFault(c, failure)
		default:
			respondError(c, 502, failure)
		}
		return
	}
	c.JSON(200, folders)
}

func (h mailboxHandlers) Subscriptions(c *gin.Context) {
	state, err := h.runtime.Subscriptions(c.Param("id"))
	if err != nil {
		respondFault(c, err)
		return
	}
	c.JSON(200, state)
}

func (h mailboxHandlers) UpdateSubscriptions(c *gin.Context) {
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
	state, err := h.runtime.UpdateSubscriptions(c.Request.Context(), c.Param("id"), mail.Subscriptions{Revision: *input.Revision, Folders: *input.Folders})
	if err != nil {
		respondFault(c, err)
		return
	}
	c.JSON(202, state)
}

// mailboxTestHandler shares input validation and probing while each client owns
// its response. App mailbox permission reveals connection success only.
func mailboxTestHandler(runtime MailboxRuntime, respond func(*gin.Context, *mail.FolderDiscovery)) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input settings.MailboxUpdate
		if !decodeRequest(c, &input) {
			return
		}
		folders, err := runtime.TestMailbox(c.Request.Context(), c.Param("id"), input)
		if err != nil {
			respondFault(c, err)
			return
		}
		respond(c, folders)
	}
}
