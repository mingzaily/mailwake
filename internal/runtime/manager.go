package runtime

import (
	"context"
	"crypto/x509"
	"log/slog"
	"maps"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mingzaily/mailwake/internal/appmanagement"
	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/engine"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/localtls"
	"github.com/mingzaily/mailwake/internal/mail"
	imapsource "github.com/mingzaily/mailwake/internal/mail/imap"
	"github.com/mingzaily/mailwake/internal/native"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

// Manager owns independent mailbox lifetimes and one global delivery dispatcher.
// Map locks only protect membership. Network tests hold no configuration locks.
type Manager struct {
	appManagement    *appmanagement.Service
	native           *native.Service
	nativeDone       chan struct{}
	mu               sync.RWMutex
	mailboxes        map[string]*mailboxRuntime
	identities       map[string]mailboxIdentity
	identitySnapshot atomic.Pointer[map[string]mailboxIdentity]
	discovery        chan struct{}
	deliveryMu       sync.RWMutex
	delivery         settings.Delivery
	deliveryInvalid  bool
	preview          atomic.Bool
	detectCodes      atomic.Bool
	store            *storage.Store
	settings         *settings.Repository
	dispatcher       *delivery.Dispatcher
	log              *slog.Logger
	ctx              context.Context
	cancel           context.CancelFunc
	workerDone       chan struct{}
	closeOnce        sync.Once
	newSource        func(settings.Mailbox) mail.Source
	newSender        func(settings.Delivery) delivery.Sender
}

// mailboxState is published after lifecycle changes. Status readers retain a
// consistent engine and configuration while a replacement waits for shutdown.
type mailboxState struct {
	config                      settings.Mailbox
	monitor                     *engine.Engine
	paused, invalid, overBudget bool
}

type mailboxRuntime struct {
	state                        atomic.Pointer[mailboxState]
	mu                           sync.RWMutex
	config                       settings.Mailbox
	invalid, deleted, overBudget bool
	budget                       *mail.ConnectionBudget
	source                       mail.Source
	monitor                      *engine.Engine
	cancel                       context.CancelFunc
	done                         chan struct{}
}

type Options struct{ IMAPRootCAs, RelayRootCAs, DeliveryRootCAs *x509.CertPool }

func New(ctx context.Context, store *storage.Store, vault *settings.Vault, log *slog.Logger, relayURL string) (*Manager, error) {
	return NewWithOptions(ctx, store, vault, log, relayURL, Options{})
}
func NewWithOptions(ctx context.Context, store *storage.Store, vault *settings.Vault, log *slog.Logger, relayURL string, options Options) (*Manager, error) {
	return newManagerWithOptions(ctx, store, vault, log, relayURL, func(m settings.Mailbox) mail.Source {
		return imapsource.NewWithOptions(m.Host, m.Port, m.Username, m.Password, log.With("mailbox_id", m.ID), imapsource.Options{RootCAs: localtls.Roots(m.Host, options.IMAPRootCAs)})
	}, func(d settings.Delivery) delivery.Sender {
		return senderWithOptions(d, options.DeliveryRootCAs)
	}, options)
}
func newManager(ctx context.Context, store *storage.Store, vault *settings.Vault, log *slog.Logger, relayURL string, sourceFactory func(settings.Mailbox) mail.Source, senderFactory func(settings.Delivery) delivery.Sender) (*Manager, error) {
	return newManagerWithOptions(ctx, store, vault, log, relayURL, sourceFactory, senderFactory, Options{})
}
func newManagerWithOptions(ctx context.Context, store *storage.Store, vault *settings.Vault, log *slog.Logger, relayURL string, sourceFactory func(settings.Mailbox) mail.Source, senderFactory func(settings.Delivery) delivery.Sender, options Options) (*Manager, error) {
	m := &Manager{store: store, settings: settings.NewRepository(store, vault), log: log, mailboxes: make(map[string]*mailboxRuntime), identities: make(map[string]mailboxIdentity), discovery: make(chan struct{}, 2), newSource: sourceFactory, newSender: senderFactory}
	saved, err := m.settings.Mailboxes(ctx)
	if err != nil {
		return nil, err
	}
	d, err := m.settings.LoadDelivery(ctx)
	if err != nil {
		if fault.From(err, "database_unavailable").Code != "configuration_invalid" {
			return nil, err
		}
		m.deliveryInvalid = true
		log.Warn("Stored configuration is invalid", "mailbox_id", "", "channel", "unconfigured", "code", "configuration_invalid")
	} else if d != nil {
		m.delivery = *d
	}
	revision, err := m.settings.DeliveryRevision(ctx)
	if err != nil {
		return nil, err
	}
	m.delivery.Revision = revision
	m.preview.Store(m.delivery.RecordsPreview())
	m.detectCodes.Store(m.delivery.Channel == "native")
	if err := store.SetDeliveryChannel(ctx, m.delivery.Channel); err != nil {
		return nil, err
	}
	// Read every saved subscription before starting any worker, so startup failures
	// cannot leave a partially running manager behind.
	states := make(map[string]mail.Subscriptions, len(saved))
	for _, s := range saved {
		state, err := store.LoadSubscriptions(ctx, s.ID)
		if err != nil {
			return nil, err
		}
		states[s.ID] = state
		b := &mailboxRuntime{config: s.Mailbox, invalid: s.Invalid, budget: mail.NewConnectionBudget(s.Limit())}
		if s.Invalid {
			log.Warn("Stored configuration is invalid", "mailbox_id", s.ID, "code", "configuration_invalid")
		} else {
			b.source = b.budget.Wrap(sourceFactory(s.Mailbox))
			m.identities[s.ID] = identityOf(s.Mailbox)
		}
		m.mailboxes[s.ID] = b
	}
	m.publishIdentities()
	m.native, err = native.NewWithOptions(store, vault, relayURL, log, native.ClientOptions{RootCAs: options.RelayRootCAs})
	if err != nil {
		return nil, err
	}
	m.appManagement = &appmanagement.Service{Store: store, Native: m.native}
	m.ctx, m.cancel = context.WithCancel(ctx)
	var channel delivery.Sender
	if m.delivery.Channel != "" {
		channel = m.deliverySender(m.delivery)
	}
	m.dispatcher = delivery.New(store, channel, m.delivery.RetryCount, log)
	m.dispatcher.Configure(channel, m.delivery.RetryCount, m.preview.Load())
	m.dispatcher.SetNativeSender(m.native)
	for id, b := range m.mailboxes {
		m.startMailbox(b, states[id])
	}
	m.nativeDone = make(chan struct{})
	go func() { defer close(m.nativeDone); m.native.Run(m.ctx) }()
	m.workerDone = make(chan struct{})
	go func() { defer close(m.workerDone); m.dispatcher.Run(m.ctx) }()
	return m, nil
}
func (b *mailboxRuntime) publish() {
	b.state.Store(&mailboxState{b.config, b.monitor, b.cancel == nil, b.invalid, b.overBudget})
}
func (m *Manager) startMailbox(b *mailboxRuntime, state mail.Subscriptions) {
	defer func() {
		b.publish()
		// Publishing before reading the policy lets a concurrent delivery update
		// either reach this engine through the snapshot or precede this read.
		b.monitor.SetPreview(m.preview.Load())
		b.monitor.SetDetectCodes(m.detectCodes.Load())
	}()
	b.monitor = engine.New(b.source, m.store, b.config.ID, state, time.Minute, m.preview.Load(), m.log)
	b.monitor.SetDetectCodes(m.detectCodes.Load())
	b.monitor.SetEventNamespace(b.config.Identity())
	b.monitor.SetMailboxLabel(b.config.Label)
	b.monitor.SetConnectionLimit(b.config.Limit())
	b.overBudget = mail.CheckConnectionBudget(state.Folders, b.config.Limit()) != nil
	if b.overBudget || b.source == nil {
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	b.cancel = cancel
	b.done = make(chan struct{})
	monitor, done, id := b.monitor, b.done, b.config.ID
	m.log.Info("Mailbox monitoring started", "mailbox_id", id)
	go func() {
		defer close(done)
		defer m.log.Info("Mailbox monitoring stopped", "mailbox_id", id)
		monitor.Run(ctx)
	}()
}
func (b *mailboxRuntime) stop() {
	if b.cancel != nil {
		b.cancel()
		<-b.done
		b.cancel = nil
	}
}
func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.cancel()
		boxes := m.snapshotLocked()
		m.mu.Unlock()
		for _, b := range boxes {
			b.mu.Lock()
			b.stop()
			b.mu.Unlock()
		}
		<-m.workerDone
		<-m.nativeDone
	})
}
func (m *Manager) snapshotLocked() []*mailboxRuntime {
	ids := make([]string, 0, len(m.mailboxes))
	for id := range m.mailboxes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	boxes := make([]*mailboxRuntime, 0, len(ids))
	for _, id := range ids {
		boxes = append(boxes, m.mailboxes[id])
	}
	return boxes
}
func (m *Manager) snapshot() []*mailboxRuntime {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshotLocked()
}
func (m *Manager) mailbox(id string) (*mailboxRuntime, error) {
	m.mu.RLock()
	b := m.mailboxes[id]
	m.mu.RUnlock()
	if b == nil {
		return nil, fault.New("mailbox_not_found")
	}
	return b, nil
}
func (m *Manager) Mailboxes() []map[string]any {
	views := []map[string]any{}
	for _, b := range m.snapshot() {
		if state := b.state.Load(); state != nil {
			views = append(views, state.config.View(b.budget.InUse()))
		}
	}
	return views
}
func (m *Manager) Mailbox(id string) (map[string]any, error) {
	b, err := m.mailbox(id)
	if err != nil {
		return nil, err
	}
	state := b.state.Load()
	if state == nil {
		return nil, fault.New("mailbox_not_found")
	}
	return state.config.View(b.budget.InUse()), nil
}
func (m *Manager) Status() []engine.FolderStatus {
	statuses := []engine.FolderStatus{}
	for _, b := range m.snapshot() {
		if state := b.state.Load(); state != nil {
			items := state.monitor.Status()
			if state.paused {
				for i := range items {
					items[i].State = "paused"
				}
			}
			statuses = append(statuses, items...)
		}
	}
	return statuses
}
func (m *Manager) Notices() map[string]*fault.Error {
	notices := make(map[string]*fault.Error)
	m.deliveryMu.RLock()
	if m.deliveryInvalid {
		notices["delivery"] = fault.New("configuration_invalid")
	}
	m.deliveryMu.RUnlock()
	for _, b := range m.snapshot() {
		if state := b.state.Load(); state != nil {
			if state.invalid {
				notices["mailbox:"+state.config.ID] = fault.New("configuration_invalid")
			}
			if state.overBudget {
				notices["subscriptions:"+state.config.ID] = fault.New("connection_budget_exceeded")
			}
		}
	}
	return notices
}
func probeMailbox(ctx context.Context, source mail.Source) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	folders, err := source.Folders(ctx)
	if err != nil {
		return nil, fault.From(err, "imap_connection_failed")
	}
	return folders, nil
}
func (m *Manager) CreateMailbox(ctx context.Context, input settings.MailboxUpdate) (map[string]any, error) {
	if err := m.beginDiscovery(); err != nil {
		return nil, err
	}
	defer m.endDiscovery()
	m.mu.RLock()
	full := len(m.mailboxes) >= storage.MaxMailboxes
	m.mu.RUnlock()
	if full {
		return nil, fault.New("mailbox_limit_exceeded")
	}
	base := settings.NewMailbox()
	next, err := base.Merge(input)
	if err != nil {
		return nil, err
	}
	b := &mailboxRuntime{config: next, budget: mail.NewConnectionBudget(next.Limit())}
	b.source = b.budget.Wrap(m.newSource(next))
	if _, err := probeMailbox(ctx, b.source); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ctx.Err(); err != nil {
		return nil, err
	}
	if err := m.checkDuplicate(next); err != nil {
		return nil, err
	}
	if err := m.settings.SaveMailboxRecord(ctx, base, next, 0); err != nil {
		return nil, err
	}
	m.identities[next.ID] = identityOf(next)
	m.publishIdentities()
	b.config.Revision = 1
	m.startMailbox(b, mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders([]string{})})
	m.mailboxes[next.ID] = b
	m.log.Info("Mailbox created", "mailbox_id", next.ID, "revision", b.config.Revision)
	return b.config.View(b.budget.InUse()), nil
}
func (m *Manager) UpdateMailbox(ctx context.Context, id string, input settings.MailboxUpdate) error {
	b, err := m.mailbox(id)
	if err != nil {
		return err
	}
	b.mu.RLock()
	current, deleted, invalid, subs := b.config, b.deleted, b.invalid, b.monitor.Subscriptions()
	b.mu.RUnlock()
	if deleted {
		return fault.New("mailbox_not_found")
	}
	if input.Revision != current.Revision {
		return fault.New("settings_conflict")
	}
	next, err := current.Merge(input)
	if err != nil {
		return err
	}
	if err := mail.CheckConnectionBudget(subs.Folders, next.Limit()); err != nil {
		return err
	}
	// Check the immutable identity snapshot before probes or watcher shutdown.
	if err := duplicateIdentity(*m.identitySnapshot.Load(), next); err != nil {
		return err
	}
	labelOnly := !invalid && input.Password == nil && next.Host == current.Host && next.Port == current.Port && next.Username == current.Username && next.Limit() == current.Limit()
	var source mail.Source
	if !labelOnly {
		source = b.budget.Wrap(m.newSource(next))
		if _, err := probeMailbox(ctx, source); err != nil {
			return err
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.deleted {
		return fault.New("mailbox_not_found")
	}
	if err := m.ctx.Err(); err != nil {
		return err
	}
	if b.config.Revision != current.Revision || (!labelOnly && b.monitor.Subscriptions().Revision != subs.Revision) {
		return fault.New("settings_conflict")
	}
	if !labelOnly {
		b.stop()
		defer func() { m.startMailbox(b, subs) }()
	}
	// Persist identity membership atomically; shutdown and restart hold only b.mu.
	if err := func() error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if err := m.checkDuplicate(next); err != nil {
			return err
		}
		if err := m.settings.SaveMailboxRecord(ctx, current, next, current.Revision); err != nil {
			return err
		}
		m.identities[id] = identityOf(next)
		m.publishIdentities()
		return nil
	}(); err != nil {
		return err
	}
	next.Revision = current.Revision + 1
	b.config = next
	m.log.Info("Mailbox updated", "mailbox_id", id, "revision", next.Revision)
	if labelOnly {
		b.monitor.SetMailboxLabel(next.Label)
		b.publish()
		return nil
	}
	b.source = source
	b.invalid = false
	b.budget.Resize(next.Limit())
	return nil
}
func (m *Manager) DeleteMailbox(ctx context.Context, id string) error {
	b, err := m.mailbox(id)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.deleted {
		return fault.New("mailbox_not_found")
	}
	if err := m.ctx.Err(); err != nil {
		return err
	}
	b.stop()
	err = func() error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if err := m.store.DeleteMailbox(ctx, id, b.config.Revision); err != nil {
			return err
		}
		b.deleted = true
		b.state.Store(nil)
		delete(m.mailboxes, id)
		delete(m.identities, id)
		m.publishIdentities()
		return nil
	}()
	if err != nil {
		m.startMailbox(b, b.monitor.Subscriptions())
		return err
	}
	m.log.Info("Mailbox deleted", "mailbox_id", id)
	return nil
}
func (m *Manager) TestMailbox(ctx context.Context, id string, input settings.MailboxUpdate) ([]string, error) {
	if err := m.beginDiscovery(); err != nil {
		return nil, err
	}
	defer m.endDiscovery()
	current := settings.Mailbox{}
	var budget *mail.ConnectionBudget
	if id != "" {
		b, err := m.mailbox(id)
		if err != nil {
			return nil, err
		}
		b.mu.RLock()
		current, budget = b.config, b.budget
		deleted := b.deleted
		b.mu.RUnlock()
		if deleted {
			return nil, fault.New("mailbox_not_found")
		}
	}
	next, err := current.Merge(input)
	if err != nil {
		return nil, err
	}
	if budget == nil {
		budget = mail.NewConnectionBudget(next.Limit())
	}
	return probeMailbox(ctx, budget.Wrap(m.newSource(next)))
}
func (m *Manager) Folders(ctx context.Context, id string) ([]string, error) {
	if err := m.beginDiscovery(); err != nil {
		return nil, err
	}
	defer m.endDiscovery()
	b, err := m.mailbox(id)
	if err != nil {
		return nil, err
	}
	b.mu.RLock()
	source, deleted := b.source, b.deleted
	b.mu.RUnlock()
	if deleted {
		return nil, fault.New("mailbox_not_found")
	}
	if source == nil {
		return nil, fault.New("mailbox_required")
	}
	return probeMailbox(ctx, source)
}

// TestSavedMailbox probes the current stored source and keeps directory data inside Core.
func (m *Manager) TestSavedMailbox(ctx context.Context, id string) error {
	_, err := m.Folders(ctx, id)
	return err
}
func (m *Manager) Subscriptions(id string) (mail.Subscriptions, error) {
	b, err := m.mailbox(id)
	if err != nil {
		return mail.Subscriptions{}, err
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.deleted {
		return mail.Subscriptions{}, fault.New("mailbox_not_found")
	}
	return b.monitor.Subscriptions(), nil
}
func (m *Manager) UpdateSubscriptions(ctx context.Context, id string, input mail.Subscriptions) (mail.Subscriptions, error) {
	b, err := m.mailbox(id)
	if err != nil {
		return mail.Subscriptions{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.deleted {
		return mail.Subscriptions{}, fault.New("mailbox_not_found")
	}
	if err := m.ctx.Err(); err != nil {
		return mail.Subscriptions{}, err
	}
	state, err := b.monitor.UpdateSubscriptions(ctx, input)
	if err == nil && b.cancel == nil {
		m.startMailbox(b, state)
	}
	if err == nil {
		m.log.Info("Subscriptions updated", "mailbox_id", id, "revision", state.Revision, "count", len(state.Folders))
	}
	return state, err
}
func (m *Manager) Channel() string {
	m.deliveryMu.RLock()
	defer m.deliveryMu.RUnlock()
	return m.delivery.Channel
}
func (m *Manager) DeliveryView() map[string]any {
	m.deliveryMu.RLock()
	defer m.deliveryMu.RUnlock()
	view := m.delivery.View()
	view["native_available"] = m.native.Available()
	return view
}
func (m *Manager) TestDelivery(ctx context.Context, input settings.DeliveryUpdate) error {
	m.deliveryMu.RLock()
	next, err := m.delivery.Merge(input)
	m.deliveryMu.RUnlock()
	if err != nil {
		return err
	}
	if next.Channel == "native" && !m.native.Available() {
		return fault.New("native_push_unavailable")
	}
	return testDelivery(ctx, m.deliverySender(next))
}

// TestSavedDelivery sends a test notification through the saved channel.
func (m *Manager) TestSavedDelivery(ctx context.Context) error {
	m.deliveryMu.RLock()
	current := m.delivery
	m.deliveryMu.RUnlock()
	if current.Channel == "" {
		return fault.New("config_delivery_channel_invalid")
	}
	if current.Channel == "native" && !m.native.Available() {
		return fault.New("native_push_unavailable")
	}
	return testDelivery(ctx, m.deliverySender(current))
}
func (m *Manager) UpdateDelivery(ctx context.Context, input settings.DeliveryUpdate) error {
	m.deliveryMu.RLock()
	current := m.delivery
	m.deliveryMu.RUnlock()
	if input.Revision == nil || *input.Revision != current.Revision {
		return fault.New("settings_conflict")
	}
	next, err := current.Merge(input)
	if err != nil {
		return err
	}
	if next.Channel == "native" && !m.native.Available() {
		return fault.New("native_push_unavailable")
	}
	channel := m.deliverySender(next)
	if current.ConnectionChanged(next) {
		if err := testDelivery(ctx, channel); err != nil {
			return err
		}
	}
	m.deliveryMu.Lock()
	defer m.deliveryMu.Unlock()
	if err := m.ctx.Err(); err != nil {
		return err
	}
	if m.delivery.Revision != current.Revision {
		return fault.New("settings_conflict")
	}
	if err := m.settings.SaveDelivery(ctx, current, next, current.Revision); err != nil {
		return err
	}
	next.Revision = current.Revision + 1
	m.delivery = next
	m.deliveryInvalid = false
	// Membership and the preview policy change together: a newly added mailbox
	// either appears in this snapshot or starts with the new policy.
	m.mu.Lock()
	m.preview.Store(next.RecordsPreview())
	m.detectCodes.Store(next.Channel == "native")
	boxes := m.snapshotLocked()
	m.mu.Unlock()
	for _, b := range boxes {
		if state := b.state.Load(); state != nil {
			state.monitor.SetPreview(m.preview.Load())
			state.monitor.SetDetectCodes(m.detectCodes.Load())
		}
	}
	m.dispatcher.Configure(channel, next.RetryCount, m.preview.Load())
	m.log.Info("Delivery settings updated", "mailbox_id", "", "channel", next.Channel, "revision", next.Revision)
	return nil
}

type mailboxIdentity struct {
	host     string
	port     int
	username string
}

func identityOf(m settings.Mailbox) mailboxIdentity {
	return mailboxIdentity{strings.ToLower(m.Host), m.Port, strings.ToLower(m.Username)}
}

// checkDuplicate runs under the manager lock, immediately before persistence.
func (m *Manager) checkDuplicate(next settings.Mailbox) error {
	return duplicateIdentity(m.identities, next)
}

// publishIdentities runs at startup and under the membership lock after each committed change.
func (m *Manager) publishIdentities() {
	snapshot := maps.Clone(m.identities)
	m.identitySnapshot.Store(&snapshot)
}

func duplicateIdentity(identities map[string]mailboxIdentity, next settings.Mailbox) error {
	target := identityOf(next)
	for id, identity := range identities {
		if id != next.ID && identity == target {
			return fault.New("mailbox_duplicate")
		}
	}
	return nil
}
func (m *Manager) beginDiscovery() error {
	select {
	case m.discovery <- struct{}{}:
		return nil
	default:
		return fault.New("discovery_busy")
	}
}
func (m *Manager) endDiscovery() { <-m.discovery }

func (m *Manager) Native() *native.Service { return m.native }

func (m *Manager) deliverySender(d settings.Delivery) delivery.Sender {
	if d.Channel == "native" {
		return m.native
	}
	return m.newSender(d)
}

func (m *Manager) AppManagement() *appmanagement.Service { return m.appManagement }

func (m *Manager) DeleteDelivery(ctx context.Context, revision int64) error {
	m.deliveryMu.Lock()
	defer m.deliveryMu.Unlock()
	if m.delivery.Revision != revision {
		return fault.New("settings_conflict")
	}
	if e := m.ctx.Err(); e != nil {
		return e
	}
	next := settings.Delivery{Revision: revision, Language: m.delivery.Language, Preview: "off"}
	if next.Language == "" {
		next.Language = "en"
	}
	if e := m.settings.SaveDelivery(ctx, m.delivery, next, revision); e != nil {
		return e
	}
	next.Revision++
	m.delivery = next
	m.deliveryInvalid = false
	m.mu.Lock()
	m.preview.Store(false)
	m.detectCodes.Store(false)
	boxes := m.snapshotLocked()
	m.mu.Unlock()
	for _, b := range boxes {
		if state := b.state.Load(); state != nil {
			state.monitor.SetPreview(false)
			state.monitor.SetDetectCodes(false)
		}
	}
	m.dispatcher.Configure(nil, 0, false)
	return nil
}
