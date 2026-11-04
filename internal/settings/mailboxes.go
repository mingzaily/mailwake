package settings

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/outlook"
	"github.com/mingzaily/mailwake/internal/storage"
	"golang.org/x/oauth2"
)

type StoredMailbox struct {
	Mailbox
	Invalid bool
}

func (r *Repository) Mailboxes(ctx context.Context) ([]StoredMailbox, error) {
	records, err := r.store.MailboxConfigurations(ctx)
	if err != nil {
		return nil, err
	}
	values := make([]StoredMailbox, 0, len(records))
	for _, record := range records {
		plain, err := r.vault.Open(record.Name, record.Encrypted)
		if err != nil {
			return nil, err
		}
		id := strings.TrimPrefix(record.Name, "mailbox:")
		var m Mailbox
		invalid := json.Unmarshal(plain, &m) != nil || m.ID != id
		if record.Outlook != nil {
			name := storage.OutlookCredentialName(id)
			refresh, err := r.vault.Open(name, record.Outlook)
			if err != nil {
				return nil, err
			}
			m.Outlook = outlook.NewGrant(oauth2.Token{RefreshToken: string(refresh)})
			r.bindOutlook(name, m.Outlook, record.Outlook)
		}
		invalid = invalid || m.validate() != nil
		if invalid {
			m = Mailbox{ID: id}
		}
		m.Revision = record.Revision
		values = append(values, StoredMailbox{Mailbox: m, Invalid: invalid})
	}
	return values, nil
}

// CreateMailboxRecord saves both initial states in one storage transaction.
func (r *Repository) CreateMailboxRecord(ctx context.Context, next Mailbox, folders []mail.Folder) error {
	return r.writeMailbox(ctx, Mailbox{}, next, 0, folders)
}

func (r *Repository) SaveMailboxRecord(ctx context.Context, current, next Mailbox, revision int64) error {
	return r.writeMailbox(ctx, current, next, revision, nil)
}

func (r *Repository) writeMailbox(ctx context.Context, current, next Mailbox, revision int64, folders []mail.Folder) error {
	data, err := r.encrypt(storage.MailboxConfigurationName(next.ID), next)
	if err != nil {
		return err
	}
	change := storage.MailboxChange{InitialFolders: folders, ResetCursors: revision > 0 && current.Identity() != next.Identity()}
	name := storage.OutlookCredentialName(next.ID)
	switch {
	case next.Outlook == nil && revision > 0:
		// A password save also repairs an invalid mailbox with a stored grant.
		change.Outlook = []byte{}
	case next.Outlook != current.Outlook:
		change.Outlook, err = r.vault.Seal(name, []byte(next.Outlook.RefreshToken()))
		if err != nil {
			return err
		}
	}
	err = r.store.WriteMailbox(ctx, next.ID, revision, data, change)
	if err == nil && change.Outlook != nil && next.Outlook != nil {
		r.bindOutlook(name, next.Outlook, change.Outlook)
	}
	return err
}

func (r *Repository) bindOutlook(name string, grant *outlook.Grant, previous []byte) {
	grant.Bind(func(ctx context.Context, refresh string) error {
		next, err := r.vault.Seal(name, []byte(refresh))
		if err != nil {
			return err
		}
		if err := r.store.RotateOutlookCredential(ctx, name, previous, next); err != nil {
			return err
		}
		previous = next
		return nil
	})
}

// SaveDelivery stores next if the saved revision still equals revision.
func (r *Repository) SaveDelivery(ctx context.Context, current, next Delivery, revision int64) error {
	encrypted, err := r.encrypt("delivery", next)
	if err != nil {
		return err
	}
	policy := next.Preview
	if next.Channel == "native" {
		policy = "subject"
	}
	return r.store.SaveDelivery(ctx, encrypted, policy, next.Channel, current.RecordsPreview() && !next.RecordsPreview(), revision)
}
func (r *Repository) DeliveryRevision(ctx context.Context) (int64, error) {
	record, err := r.store.ConfigurationRecord(ctx, "delivery")
	return record.Revision, err
}

// RevokeNativePairing resets the selected native target while retaining other channel settings.
func (r *Repository) RevokeNativePairing(ctx context.Context, id string, unlink bool, current Delivery) (Delivery, error) {
	next := current
	var change *storage.DeliveryChange
	if current.Channel == "native" && current.NativePairingID == id {
		next.Channel, next.NativePairingID = "", ""
		encrypted, err := r.encrypt("delivery", next)
		if err != nil {
			return current, err
		}
		change = &storage.DeliveryChange{Encrypted: encrypted, Preview: next.Preview, Channel: next.Channel, ClearPreview: current.RecordsPreview() && !next.RecordsPreview(), Revision: current.Revision}
		next.Revision++
	}
	_, err := r.store.RevokeNativePairingWithDelivery(ctx, id, unlink, change)
	return next, err
}

// RefreshMailboxDisplay stores server-derived presentation data without changing configuration revision.
func (r *Repository) RefreshMailboxDisplay(ctx context.Context, value Mailbox) error {
	encrypted, err := r.encrypt(storage.MailboxConfigurationName(value.ID), value)
	if err != nil {
		return err
	}
	return r.store.RefreshMailboxDisplay(ctx, value.ID, value.Revision, encrypted)
}
