package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/logging"
)

func appRequestDiagnostics(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// FullPath is a registered template, never a raw URL or resource identity.
		route := c.FullPath()
		if route == "" && (c.Request.URL.Path == appPrefix || strings.HasPrefix(c.Request.URL.Path, appPrefix+"/")) {
			route = appPrefix + "/*unmatched"
		}
		if !strings.HasPrefix(route, appPrefix+"/") {
			c.Next()
			return
		}
		method := c.Request.Method
		switch method {
		case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace:
		default:
			method = "OTHER"
		}
		started := time.Now()
		id := make([]byte, 16)
		_, _ = rand.Read(id)
		requestID := hex.EncodeToString(id)
		ctx, trace := logging.WithRequestTrace(c.Request.Context())
		c.Request = c.Request.WithContext(ctx)
		c.Header("X-Request-ID", requestID)
		c.Next()
		status := c.Writer.Status()
		outcome := trace.Outcome
		if status >= 400 {
			outcome = "failed"
		} else if outcome == "" {
			outcome = "success"
		}
		level := slog.LevelInfo
		if status >= 400 {
			level = slog.LevelWarn
		}
		log.Log(ctx, level, logging.AppRequestMessage,
			"request_id", requestID, "method", method, "route", route,
			"http_status", status, "duration_ms", time.Since(started).Milliseconds(),
			"stage", trace.Stage, "outcome", outcome, "code", c.GetString("diagnostic_error_code"),
			"cause_code", trace.CauseCode, "response_code", trace.ResponseCode)
	}
}
