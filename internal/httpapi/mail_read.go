package httpapi

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mailview"
	"github.com/mingzaily/mailwake/internal/storage"
)

func appMailRoutes(app appRouteRegistrar, runtime MailReadRuntime) {
	app.HandleController("GET", "/capabilities", "", func(c *gin.Context, controller storage.AppController) {
		c.Header("Cache-Control", "no-store")
		result, err := runtime.MailCapabilities(c.Request.Context())
		if err != nil {
			respondFault(c, err)
			return
		}
		result.Permissions.Content = controller.HasScope("content")
		c.JSON(200, result)
	})
	app.HandleController("GET", "/mail/folders", "content", func(c *gin.Context, controller storage.AppController) {
		c.Header("Cache-Control", "no-store")
		limit, ok := mailPageQuery(c)
		if !ok {
			return
		}
		result, err := runtime.MailFolders(c.Request.Context(), mailReader(controller), limit, c.Query("cursor"))
		if err != nil {
			respondFault(c, err)
			return
		}
		c.JSON(200, result)
	})
	app.HandleController("GET", "/mail/health", "content", func(c *gin.Context, controller storage.AppController) {
		c.Header("Cache-Control", "no-store")
		limit, ok := mailPageQuery(c)
		if !ok {
			return
		}
		result, err := runtime.MailHealth(c.Request.Context(), mailReader(controller), limit, c.Query("cursor"))
		if err != nil {
			respondFault(c, err)
			return
		}
		c.JSON(200, result)
	})
	app.HandleController("POST", "/mail/messages", "content", func(c *gin.Context, controller storage.AppController) {
		c.Header("Cache-Control", "no-store")
		input := mailview.MessageRequest{Limit: mailview.DefaultPageSize}
		if !decodeRequest(c, &input) {
			return
		}
		if input.MailboxID == "" || input.AccountID == "" || input.Folder == "" || input.Limit < 1 || input.Limit > mailview.MaxPageSize {
			respondFault(c, fault.New("request_invalid"))
			return
		}
		result, err := runtime.MailMessages(c.Request.Context(), mailReader(controller), input)
		if err != nil {
			respondFault(c, err)
			return
		}
		c.JSON(200, result)
	})
}

func mailReader(controller storage.AppController) mailview.Reader {
	return mailview.Reader{ControllerID: controller.ID, DeviceID: controller.DeviceID}
}

func mailPageQuery(c *gin.Context) (int, bool) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", strconv.Itoa(mailview.DefaultPageSize)))
	if err != nil || limit < 1 || limit > mailview.MaxPageSize {
		respondFault(c, fault.New("request_invalid"))
		return 0, false
	}
	return limit, true
}
