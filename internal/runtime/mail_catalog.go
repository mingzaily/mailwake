package runtime

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mailview"
)

func (m *Manager) MailCapabilities(ctx context.Context) (mailview.Capabilities, error) {
	identity, err := m.identity.Load(ctx)
	if err != nil {
		return mailview.Capabilities{}, err
	}
	return mailview.Capabilities{
		CoreID:      identity.ID,
		MailHistory: mailview.HistoryCapability{Version: 1, WindowDays: 30, MaxPageSize: mailview.MaxPageSize},
		MailHealth:  mailview.HealthCapability{Version: 1},
	}, nil
}

func (m *Manager) MailFolders(ctx context.Context, reader mailview.Reader, limit int, token string) (mailview.Page[mailview.Folder], error) {
	health, err := m.catalogPage(ctx, reader, limit, token, "folders")
	if err != nil {
		return mailview.Page[mailview.Folder]{}, err
	}
	page := mailview.Page[mailview.Folder]{CoreID: health.CoreID, Items: make([]mailview.Folder, 0, len(health.Items)), NextCursor: health.NextCursor, Complete: health.Complete}
	for _, item := range health.Items {
		page.Items = append(page.Items, item.Folder)
	}
	return page, nil
}

func (m *Manager) MailHealth(ctx context.Context, reader mailview.Reader, limit int, token string) (mailview.Page[mailview.Health], error) {
	return m.catalogPage(ctx, reader, limit, token, "health")
}

func (m *Manager) mailCatalog() []mailview.Health {
	items := []mailview.Health{}
	for _, box := range m.snapshot() {
		state := box.state.Load()
		if state == nil {
			continue
		}
		revision, statuses := state.monitor.StatusSnapshot()
		for _, status := range statuses {
			folder := mailview.Folder{
				MailboxID: state.config.ID, AccountID: state.config.Identity(), MailboxLabel: state.config.Label,
				Folder: status.Folder, SubscriptionRevision: revision,
			}
			if display := state.config.FolderDisplay; display != nil {
				folder.FolderName = display.FolderNames[status.Folder]
				folder.FolderRole = display.FolderRoles[status.Folder]
			}
			item := mailview.Health{Folder: folder, State: "starting", UpdatedAt: status.UpdatedAt, Check: status.Check, LastCheckedAt: status.LastCheck}
			switch {
			case state.paused:
				item.State, item.ReasonCode = "paused", "configuration_invalid"
				if state.overBudget {
					item.ReasonCode = "connection_budget_exceeded"
				}
			case status.State == "watching":
				item.State = "healthy"
			case status.State == "auth_required":
				item.State, item.ReasonCode = "action_required", healthReason(status.LastError.Code)
			case status.State == "reconnecting":
				item.State, item.ReasonCode = "retrying", healthReason(status.LastError.Code)
			}
			items = append(items, item)
		}
	}
	slices.SortFunc(items, func(a, b mailview.Health) int {
		if n := cmp.Compare(a.MailboxID, b.MailboxID); n != 0 {
			return n
		}
		return cmp.Compare(a.Folder.Folder, b.Folder.Folder)
	})
	return items
}

func healthReason(code string) string {
	switch code {
	case "mail_auth_failed", "outlook_reauthorize", "imap_connection_failed", "imap_connection_closed", "imap_command_timeout", "imap_login_failed", "imap_capability_failed", "imap_folder_open_failed", "imap_folder_rejected", "imap_status_failed", "imap_metadata_failed", "imap_idle_start_failed", "outlook_unavailable", "connection_budget_exceeded", "checkpoint_storage_failed":
		return code
	}
	return "mail_check_failed"
}

func catalogIdentity(items []mailview.Health) string {
	identities := make([]any, 0, len(items))
	for _, item := range items {
		identities = append(identities, []any{item.MailboxID, item.AccountID, item.Folder.Folder, item.SubscriptionRevision})
	}
	raw, _ := json.Marshal(identities)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (m *Manager) catalogPage(ctx context.Context, reader mailview.Reader, limit int, token, kind string) (mailview.Page[mailview.Health], error) {
	identity, err := m.identity.Load(ctx)
	if err != nil {
		return mailview.Page[mailview.Health]{}, err
	}
	now := time.Now().UTC()
	items := m.mailCatalog()
	digest := catalogIdentity(items)
	cursor := mailCursor{Version: 1, Kind: kind, Reader: reader, Expires: now.Add(15 * time.Minute), Catalog: digest}
	if token != "" {
		cursor, err = readMailCursor(identity, token, kind, reader, now)
		if err != nil {
			return mailview.Page[mailview.Health]{}, err
		}
		if cursor.Catalog != digest {
			return mailview.Page[mailview.Health]{}, fault.New("sync_cursor_invalid")
		}
	}
	start, end := cursor.Offset, min(cursor.Offset+limit, len(items))
	page := mailview.Page[mailview.Health]{CoreID: identity.ID, ObservedAt: &now}
	for {
		page.Items, page.Complete = items[start:end], end == len(items)
		page.NextCursor = ""
		cursor.Offset = end
		if !page.Complete {
			page.NextCursor, err = cursor.sign(identity)
			if err != nil {
				return mailview.Page[mailview.Health]{}, err
			}
		}
		raw, err := json.Marshal(page)
		if err != nil {
			return mailview.Page[mailview.Health]{}, err
		}
		if len(raw) <= mailview.MaxResponseBytes {
			break
		}
		if end-start <= 1 {
			return mailview.Page[mailview.Health]{}, fault.New("sync_page_too_large")
		}
		end = start + max(1, (end-start)*mailview.MaxResponseBytes/len(raw)-1)
	}
	if digest != catalogIdentity(m.mailCatalog()) {
		return mailview.Page[mailview.Health]{}, fault.New("sync_cursor_invalid")
	}
	return page, nil
}
