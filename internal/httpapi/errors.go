package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/fault"
)

// respondFault maps stable business codes at the HTTP boundary. Raw internal
// errors use the database-unavailable fallback and never reach the response.
func respondFault(c *gin.Context, err error) {
	failure := fault.From(err, "database_unavailable")

	status := http.StatusBadRequest
	switch failure.Code {
	case "discovery_busy":
		status = http.StatusTooManyRequests
		c.Header("Retry-After", "1")
	case "too_many_attempts":
		status = http.StatusTooManyRequests
		c.Header("Retry-After", failure.Params["retry_after"])
	case "database_unavailable", "configuration_corrupt", "subscriptions_storage_failed":
		status = http.StatusServiceUnavailable
	case "message_not_found", "mailbox_not_found", "invitation_not_found", "controller_not_found", "native_pairing_not_found":
		status = http.StatusNotFound
	case "invitation_used", "invitation_expired":
		status = http.StatusGone
	case "native_push_required", "content_permission_required", "app_management_required", "forbidden":
		status = http.StatusForbidden
	case "message_timeout":
		status = http.StatusGatewayTimeout
	case "message_content_invalid":
		status = http.StatusUnprocessableEntity
	case "message_too_large":
		status = http.StatusRequestEntityTooLarge
	case "message_unavailable", "app_management_unavailable", "native_push_unavailable":
		status = http.StatusServiceUnavailable
	case "mailbox_duplicate", "setup_complete", "settings_conflict", "subscriptions_conflict":
		status = http.StatusConflict
	case "unauthorized":
		status = http.StatusUnauthorized
	case "setup_required", "csrf_invalid":
		status = http.StatusForbidden
	case "setup_code_invalid", "current_password_invalid", "configuration_invalid", "connection_budget_exceeded", "connection_limit_invalid", "mailbox_required", "delivery_test_failed",
		"credential_required", "config_imap_address_invalid", "config_account_required", "config_language_invalid", "config_retry_count_invalid",
		"config_preview_invalid", "config_bark_endpoint_invalid", "config_webhook_url_invalid", "config_webhook_secret_invalid", "config_delivery_channel_invalid",
		"request_invalid", "scope_invalid", "core_origin_invalid":
		status = http.StatusBadRequest
	}
	respondError(c, status, failure)
}
