package httpapi

import (
	"log/slog"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/logging"
)

func logsRoute(api *gin.RouterGroup, log *slog.Logger) {
	api.DELETE("/logs", func(c *gin.Context) {
		c.JSON(200, gin.H{"cleared_through": logging.Clear(log)})
	})
	api.GET("/logs", func(c *gin.Context) {
		q := logging.Query{Limit: 200, MailboxID: c.Query("mailbox_id")}
		invalid := func() { respondError(c, 400, fault.New("logs_request_invalid")) }
		if value, ok := c.GetQuery("after"); ok {
			n, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				invalid()
				return
			}
			q.After = n
		}
		if value, ok := c.GetQuery("limit"); ok {
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 500 {
				invalid()
				return
			}
			q.Limit = n
		}
		switch c.DefaultQuery("level", "info") {
		case "info":
			q.Level = slog.LevelInfo
		case "warn":
			q.Level = slog.LevelWarn
		case "error":
			q.Level = slog.LevelError
		default:
			invalid()
			return
		}
		c.JSON(200, logging.Read(log, q))
	})
}
