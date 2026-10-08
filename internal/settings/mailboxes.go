package settings

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/mingzaily/mailwake/internal/storage"
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
		invalid := json.Unmarshal(plain, &m) != nil || m.ID != id || m.validate() != nil
		if invalid {
			m = Mailbox{ID: id}
		}
		m.Revision = record.Revision
		values = append(values, StoredMailbox{Mailbox: m, Invalid: invalid})
	}
	return values, nil
}

// SaveMailboxRecord stores next if the saved revision still equals revision (0 creates it).
func (r *Repository) SaveMailboxRecord(ctx context.Context, current, next Mailbox, revision int64) error {
	encrypted, err := r.encrypt(storage.MailboxConfigurationName(next.ID), next)
	if err != nil {
		return err
	}
	return r.store.SaveMailboxRecord(ctx, next.ID, revision, encrypted, current.Identity() != next.Identity())
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
