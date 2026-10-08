package bark

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/i18n"
)

func TestNotificationLanguage(t *testing.T) {
	for _, locale := range []string{"en", "zh-CN"} {
		for _, test := range []bool{false, true} {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get(delivery.HeaderEvent) != "test_fixture" {
					t.Error("outgoing test ID changed")
				}
				var payload struct{ Title, Body, Group string }
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					return
				}
				body := "Private subject"
				title := "工作邮箱 · 客户"
				if test {
					body = i18n.Message(locale, "notification.test_body", nil)
					title = i18n.Message(locale, "notification.test_title", nil)
				}
				if payload.Body != body || payload.Title != title || payload.Group != title {
					t.Errorf("%s test=%v: %+v", locale, test, payload)
				}
				w.Write([]byte(`{"code":200}`))
			}))
			s := New(server.URL, "key", locale)
			s.client.Transport = server.Client().Transport
			if err := s.Send(context.Background(), event.Notification{ID: "test_fixture", Account: "工作邮箱", MailboxID: "mbx_work", Folder: "客户", Subject: "Private subject", Sender: "hidden@example.org", Test: test}); err != nil {
				t.Error(err)
			}
			server.Close()
		}
	}
}

func TestBarkResponseClassification(t *testing.T) {
	for _, tc := range []struct {
		name           string
		status         int
		body           string
		retry, success bool
	}{
		{"accepted", 200, `{"code":200}`, false, true},
		{"invalid key", 200, `{"code":400,"message":"secret-device-key"}`, false, false},
		{"rate limited", 429, `secret-device-key`, true, false},
		{"temporary failure", 503, `secret-device-key`, true, false},
		{"malformed", 200, `broken secret-device-key`, true, false},
		{"redirect", 307, `secret-device-key`, false, false},
		{"empty", 204, ``, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/push" || r.URL.RawQuery != "" {
					t.Errorf("请求方式或路径错误: %s %s", r.Method, r.URL)
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if payload["device_key"] != "secret-device-key" {
					t.Errorf("请求字段错误: %+v", payload)
				}
				if archive, exists := payload["isArchive"]; exists {
					t.Errorf("Bark history uses the app setting; payload includes isArchive=%v", archive)
				}
				w.Header().Set("Retry-After", "30")
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			s := New(server.URL, "secret-device-key", "en")
			s.client.Transport = server.Client().Transport
			err := s.Send(context.Background(), event.Notification{Folder: "Clients"})
			if tc.success {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var failure *delivery.Failure
			if !errors.As(err, &failure) || failure.Retryable != tc.retry || strings.Contains(err.Error(), "secret-device-key") {
				t.Fatalf("错误分类或脱敏失败: %v", err)
			}
		})
	}
}

func TestRedirectNeverForwardsCredentials(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1); w.Write([]byte(`{"code":200}`)) }))
	defer target.Close()
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	s := New(origin.URL, "key", "en")
	s.client.Transport = origin.Client().Transport
	if err := s.Send(context.Background(), event.Notification{}); err == nil {
		t.Fatal("重定向应失败")
	}
	if forwarded.Load() != 0 {
		t.Fatal("密钥被转发到其他服务")
	}
}

func TestPreviewUTF8AndPayloadLimit(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Title, Body, Group string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if !utf8.ValidString(body.Title) || !utf8.ValidString(body.Body) || len(body.Title)+len(body.Body) > 1000 {
			t.Errorf("预览长度或编码错误: %d %d", len(body.Title), len(body.Body))
		}
		if strings.Contains(body.Body, "发件人") || !strings.Contains(body.Body, "主题") || body.Group != body.Title {
			t.Error("预览只含主题，分组与截断后的标题一致")
		}
		w.Write([]byte(`{"code":200}`))
	}))
	defer server.Close()
	s := New(server.URL, "key", "en")
	s.client.Transport = server.Client().Transport
	if err := s.Send(context.Background(), event.Notification{Folder: strings.Repeat("文件夹", 100), Sender: strings.Repeat("发件人", 100), Subject: strings.Repeat("主题", 1000)}); err != nil {
		t.Fatal(err)
	}
}

func TestRetryAfterPreservesServerDelay(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusTooManyRequests} {
		for _, header := range []string{"3600", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)} {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", header)
				w.WriteHeader(status)
				w.Write([]byte(`{"code":429}`))
			}))
			s := New(server.URL, "key", "en")
			s.client.Transport = server.Client().Transport
			err := s.Send(t.Context(), event.Notification{})
			server.Close()
			var failure *delivery.Failure
			if !errors.As(err, &failure) || !failure.Retryable || failure.RetryAfter < 59*time.Minute || failure.RetryAfter > time.Hour {
				t.Fatalf("status=%d header=%q: %+v", status, header, err)
			}
		}
	}
}
