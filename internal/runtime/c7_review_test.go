package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/auth"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/httpapi"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/settings"
)

type slowStopSource struct {
	*multiSource
	stopping, release chan struct{}
	notify            *sync.Once
}

func (s slowStopSource) Open(context.Context, string) (mail.Session, error) {
	s.opened.Add(1)
	return slowStopSession{multiSession{s.multiSource}, s.stopping, s.release, s.notify}, nil
}

type slowStopSession struct {
	multiSession
	stopping, release chan struct{}
	notify            *sync.Once
}

func (s slowStopSession) Close() error {
	s.notify.Do(func() { close(s.stopping) })
	<-s.release
	return s.multiSession.Close()
}

func TestSlowMailboxStopKeepsOtherMailboxAndStatusAvailable(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "update", true: "delete"}[remove], func(t *testing.T) {
			stopping, release := make(chan struct{}), make(chan struct{})
			slow := slowStopSource{&multiSource{}, stopping, release, &sync.Once{}}
			m, store, _ := managerFixture(t, func(c settings.Mailbox) mail.Source {
				if c.Host == "first" && c.Password == "test-password" {
					return slow
				}
				return &multiSource{}
			})
			service := auth.New(store, m.log)
			code, err := service.Prepare(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			grant, err := service.Setup(t.Context(), "local", code, "c7-admin", "synthetic-c7-password")
			if err != nil {
				t.Fatal(err)
			}
			router := httpapi.New(service, m, store, m.log)
			// Release before manager cleanup, including when a deadline assertion fails.
			defer close(release)
			a, input := addMailbox(t, m, "first", 10)
			b, other := addMailbox(t, m, "second", 10)
			subscribeMailbox(t, m, a)
			waitFor(t, func() bool { return slow.opened.Load() == 1 })
			done := make(chan error, 1)
			go func() {
				if remove {
					done <- m.DeleteMailbox(t.Context(), a)
					return
				}
				input.Password = ptr("replacement")
				done <- m.UpdateMailbox(t.Context(), a, input)
			}()
			select {
			case <-stopping:
			case <-time.After(3 * time.Second):
				t.Fatal("stop never started")
			}
			available := make(chan error, 1)
			go func() {
				if _, err := m.Mailbox(b); err != nil {
					available <- err
					return
				}
				other.Password = nil
				other.Label = ptr("renamed")
				if err := m.UpdateMailbox(t.Context(), b, other); err != nil {
					available <- err
					return
				}
				revision := int64(0)
				delivery := settings.DeliveryUpdate{Revision: &revision, Channel: "bark", Preview: "subject"}
				delivery.Bark.Key = ptr("synthetic")
				if err := m.UpdateDelivery(t.Context(), delivery); err != nil {
					available <- err
					return
				}
				req := httptest.NewRequest("GET", "/api/v1/status", nil)
				req.AddCookie(&http.Cookie{Name: "mailwake_session", Value: grant.ID})
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				if response.Code != 200 {
					available <- fmt.Errorf("status response: %d", response.Code)
					return
				}
				m.Mailboxes()
				available <- nil
			}()
			select {
			case err := <-available:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("unrelated reads, writes or status waited for stop")
			}
			select {
			case err := <-done:
				t.Fatalf("shutdown gate ineffective: %v", err)
			default:
			}
		})
	}
}

func TestCreateMailboxSharesTestAdmission(t *testing.T) {
	started, release := make(chan struct{}, 2), make(chan struct{})
	m, store, _ := managerFixture(t, func(settings.Mailbox) mail.Source { return admissionSource{started, release} })
	service := auth.New(store, m.log)
	code, err := service.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.Setup(t.Context(), "local", code, "c7-admin", "synthetic-c7-password")
	if err != nil {
		t.Fatal(err)
	}
	router := httpapi.New(service, m, store, m.log)
	input := settings.MailboxUpdate{Host: "imap.test", Port: 993, Username: "user", Password: ptr("synthetic")}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 2)
	go func() { _, err := m.CreateMailbox(ctx, input); done <- err }()
	go func() { _, err := m.TestMailbox(ctx, "", input); done <- err }()
	<-started
	<-started
	if _, err := m.CreateMailbox(ctx, input); fault.From(err, "").Code != "discovery_busy" {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "http://core.test/api/v1/mailboxes", strings.NewReader(`{"host":"imap.test","port":993,"username":"user","password":"synthetic"}`))
	req.AddCookie(&http.Cookie{Name: "mailwake_session", Value: grant.ID})
	req.Header.Set("Origin", "http://core.test")
	req.Header.Set("X-CSRF-Token", grant.CSRF)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != 429 || !strings.Contains(response.Body.String(), "discovery_busy") || response.Header().Get("Retry-After") != "1" {
		t.Fatal(response.Code, response.Body.String())
	}
	cancel()
	<-done
	<-done
	close(release)
	if _, err := m.CreateMailbox(t.Context(), input); err != nil {
		t.Fatal("admission leaked after cancellation", err)
	}
}

type admissionSource struct {
	started chan struct{}
	release chan struct{}
}

func (s admissionSource) Folders(ctx context.Context) (*mail.FolderDiscovery, error) {
	s.started <- struct{}{}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.release:
		return &mail.FolderDiscovery{Folders: []string{"INBOX"}}, nil
	}
}
func (admissionSource) Open(context.Context, string) (mail.Session, error) {
	return multiSession{&multiSource{}}, nil
}

func TestDuplicateUpdatePreservesLiveMailbox(t *testing.T) {
	source := &multiSource{}
	m, store, _ := managerFixture(t, func(settings.Mailbox) mail.Source { return source })
	id, input := addMailbox(t, m, "original", 10)
	addMailbox(t, m, "occupied", 10)
	subscribeMailbox(t, m, id)
	waitFor(t, func() bool { cp, _ := store.Checkpoint(t.Context(), id, "INBOX"); return cp == "progress" })
	before := m.mailboxes[id].state.Load()
	input.Host = "OCCUPIED"
	if err := m.UpdateMailbox(t.Context(), id, input); fault.From(err, "").Code != "mailbox_duplicate" {
		t.Fatal(err)
	}
	if source.closed.Load() != 0 || source.opened.Load() != 1 || m.mailboxes[id].state.Load().monitor != before.monitor {
		t.Fatal("rejected duplicate restarted mailbox monitoring")
	}
	view, err := m.Mailbox(id)
	if err != nil || view["revision"] != int64(1) {
		t.Fatal("rejected update changed revision", view, err)
	}
	if cp, _ := store.Checkpoint(t.Context(), id, "INBOX"); cp != "progress" {
		t.Fatal("rejected update changed progress")
	}
}

func (s admissionSource) ReadContent(context.Context, string, mail.Location) (mail.Body, error) {
	return mail.Body{}, nil
}
