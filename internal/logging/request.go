package logging

import (
	"context"
	"log/slog"
	"time"
)

// RequestTrace carries only fixed operational codes. It belongs to the synchronous
// request flow; protocol reader goroutines never access it.
type RequestTrace struct {
	Stage        string
	Outcome      string
	CauseCode    string
	ResponseCode string
}
type requestTraceKey struct{}

func WithRequestTrace(ctx context.Context) (context.Context, *RequestTrace) {
	trace := &RequestTrace{Stage: "request"}
	return context.WithValue(ctx, requestTraceKey{}, trace), trace
}
func RequestStage(ctx context.Context, stage string) {
	if trace, ok := ctx.Value(requestTraceKey{}).(*RequestTrace); ok {
		trace.Stage = stage
	}
}
func RequestOutcome(ctx context.Context, outcome string) {
	if trace, ok := ctx.Value(requestTraceKey{}).(*RequestTrace); ok {
		trace.Outcome = outcome
	}
}
func RequestCause(ctx context.Context, code string) {
	if trace, ok := ctx.Value(requestTraceKey{}).(*RequestTrace); ok {
		trace.CauseCode = code
	}
}

// RequestResponseCode accepts known IMAP codes, excluding provider-defined data.
func RequestResponseCode(ctx context.Context, code string) {
	switch code {
	case "NONEXISTENT", "NOPERM", "UNAVAILABLE", "AUTHENTICATIONFAILED", "AUTHORIZATIONFAILED", "EXPIRED", "PRIVACYREQUIRED", "CONTACTADMIN", "CANNOT", "LIMIT", "OVERQUOTA", "SERVERBUG", "CLIENTBUG", "INUSE", "READ-ONLY":
	default:
		code = "OTHER"
	}
	if trace, ok := ctx.Value(requestTraceKey{}).(*RequestTrace); ok {
		trace.ResponseCode = code
	}
}

const AppRequestMessage = "App request completed"
const AppRequestLimit = 300

// AppRequest is the shareable projection; identities, parameters and free text
// from ordinary console events stay outside the diagnostic export.
type AppRequest struct {
	Time         time.Time `json:"time"`
	RequestID    string    `json:"request_id"`
	Method       string    `json:"method"`
	Route        string    `json:"route"`
	Status       int64     `json:"http_status"`
	DurationMS   int64     `json:"duration_ms"`
	Stage        string    `json:"stage"`
	Outcome      string    `json:"outcome"`
	Code         string    `json:"code,omitempty"`
	CauseCode    string    `json:"cause_code,omitempty"`
	ResponseCode string    `json:"response_code,omitempty"`
}

func AppRequests(log *slog.Logger) []AppRequest {
	result := []AppRequest{}
	h, ok := log.Handler().(*handler)
	if !ok {
		return result
	}
	for _, entry := range h.read(Query{Limit: Capacity}).Entries {
		if entry.Message != AppRequestMessage {
			continue
		}
		str := func(key string) string { value, _ := entry.Attrs[key].(string); return value }
		number := func(key string) int64 { value, _ := entry.Attrs[key].(int64); return value }
		result = append(result, AppRequest{
			Time: entry.Time, RequestID: str("request_id"),
			Method: str("method"), Route: str("route"), Status: number("http_status"),
			DurationMS: number("duration_ms"), Stage: str("stage"), Outcome: str("outcome"),
			Code: str("code"), CauseCode: str("cause_code"), ResponseCode: str("response_code"),
		})
	}
	if len(result) > AppRequestLimit {
		result = result[len(result)-AppRequestLimit:]
	}
	return result
}
