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

	"github.com/mingzaily/mailwake/internal/coreidentity"
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
	identity         *coreidentity.Store
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

type Options struct{ IMAPRootCAs, DeliveryRootCAs *x509.CertPool }

// Dependencies are assembled before the manager starts its workers.
type Dependencies struct {
	Identity *coreidentity.Store
	Native   *native.Service
}

func New(ctx context.Context, store *storage.Store, vault *settings.Vault, log *slog.Logger, dependencies Dependencies, options Options) (*Manager, error) {
	return newManagerWithFactories(ctx, store, vault, log, dependencies, func(m settings.Mailbox) mail.Source {
		return imapsource.NewWithOptions(m.Host, m.Port, m.Username, m.Password, log.With("mailbox_id", m.ID), imapsource.Options{RootCAs: localtls.Roots(m.Host, options.IMAPRootCAs)})
	}, func(d settings.Delivery) delivery.Sender {
		return senderWithOptions(d, options.DeliveryRootCAs)
	})
}
func newManagerWithFactories(ctx context.Context, store *storage.Store, vault *settings.Vault, log *slog.Logger, dependencies Dependencies, sourceFactory func(settings.Mailbox) mail.Source, senderFactory func(settings.Delivery) delivery.Sender) (*Manager, error) {
	m := &Manager{identity: dependencies.Identity, native: dependencies.Native, store: store, settings: settings.NewRepository(store, vault), log: log, mailboxes: make(map[string]*mailboxRuntime), identities: make(map[string]mailboxIdentity), discovery: make(chan struct{}, 2), newSource: sourceFactory, newSender: senderFactory}
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
	m.native.SetPairingRevoker(m.revokeNativePairing)
	m.ctx, m.cancel = context.WithCancel(ctx)
	var channel delivery.Sender
	if m.delivery.Channel != "" {
		channel = m.deliverySender(m.delivery)
	}
	m.dispatcher = delivery.New(store, channel, m.delivery.RetryCount, log)
	m.dispatcher.Configure(channel, m.delivery.RetryCount, m.delivery.RecordsPreview())
	m.dispatcher.SetNativeSender(m.native.ForPairing(m.delivery.NativePairingID))
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
		// Publishing before reading code detection lets a concurrent delivery update
		// either reach this engine through the snapshot or precede this read.
		b.monitor.SetDetectCodes(m.detectCodes.Load())
	}()
	b.monitor = engine.New(b.source, m.store, b.config.ID, state, time.Minute, m.log)
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
func (m *Manager) Mailboxes() []settings.MailboxView {
	views := []settings.MailboxView{}
	for _, b := range m.snapshot() {
		if state := b.state.Load(); state != nil {
			views = append(views, state.config.View(b.budget.InUse()))
		}
	}
	return views
}
func (m *Manager) Mailbox(id string) (settings.MailboxView, error) {
	b, err := m.mailbox(id)
	if err != nil {
		return settings.MailboxView{}, err
	}
	state := b.state.Load()
	if state == nil {
		return settings.MailboxView{}, fault.New("mailbox_not_found")
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
func probeMailbox(ctx context.Context, source mail.Source) (*mail.FolderDiscovery, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	folders, err := source.Folders(ctx)
	if err != nil {
		return nil, fault.From(err, "imap_connection_failed")
	}
	return folders, nil
}
func (m *Manager) CreateMailbox(ctx context.Context, input settings.MailboxCreate) (settings.MailboxView, error) {
	if err := m.beginDiscovery(); err != nil {
		return settings.MailboxView{}, err
	}
	defer m.endDiscovery()
	m.mu.RLock()
	full := len(m.mailboxes) >= storage.MaxMailboxes
	m.mu.RUnlock()
	if full {
		return settings.MailboxView{}, fault.New("mailbox_limit_exceeded")
	}
	base := settings.NewMailbox()
	next, err := base.Merge(input.MailboxUpdate)
	if err != nil {
		return settings.MailboxView{}, err
	}
	folders, err := mail.NormalizeSubscriptions(input.Folders)
	if err != nil {
		return settings.MailboxView{}, err
	}
	if err := mail.CheckConnectionBudget(folders, next.Limit()); err != nil {
		return settings.MailboxView{}, err
	}
	b := &mailboxRuntime{config: next, budget: mail.NewConnectionBudget(next.Limit())}
	b.source = b.budget.Wrap(m.newSource(next))
	directory, err := probeMailbox(ctx, b.source)
	if err != nil {
		return settings.MailboxView{}, err
	}
	available := make(map[string]bool, len(directory.Folders))
	for _, name := range directory.Folders {
		available[name] = true
	}
	for _, folder := range folders {
		if !available[folder.Name] {
			return settings.MailboxView{}, fault.New("config_folders_invalid")
		}
	}
	next.FolderDisplay = directory.Display()
	b.config = next
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ctx.Err(); err != nil {
		return settings.MailboxView{}, err
	}
	if err := m.checkDuplicate(next); err != nil {
		return settings.MailboxView{}, err
	}
	if err := m.settings.CreateMailboxRecord(ctx, next, folders); err != nil {
		return settings.MailboxView{}, err
	}
	m.identities[next.ID] = identityOf(next)
	m.publishIdentities()
	b.config.Revision = 1
	m.startMailbox(b, mail.Subscriptions{Revision: 1, Folders: folders})
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
		directory, err := probeMailbox(ctx, source)
		if err != nil {
			return err
		}
		next.FolderDisplay = directory.Display()
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
	if labelOnly {
		next.FolderDisplay = b.config.FolderDisplay
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
func (m *Manager) TestMailbox(ctx context.Context, id string, input settings.MailboxUpdate) (*mail.FolderDiscovery, error) {
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
func (m *Manager) Folders(ctx context.Context, id string) (*mail.FolderDiscovery, error) {
	if err := m.beginDiscovery(); err != nil {
		return nil, err
	}
	defer m.endDiscovery()
	b, err := m.mailbox(id)
	if err != nil {
		return nil, err
	}
	b.mu.RLock()
	source, deleted, revision := b.source, b.deleted, b.config.Revision
	b.mu.RUnlock()
	if deleted {
		return nil, fault.New("mailbox_not_found")
	}
	if source == nil {
		return nil, fault.New("mailbox_required")
	}
	directory, err := probeMailbox(ctx, source)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.deleted {
		return nil, fault.New("mailbox_not_found")
	}
	if b.config.Revision != revision {
		return nil, fault.New("settings_conflict")
	}
	next := b.config
	next.FolderDisplay = directory.Display()
	if err := m.settings.RefreshMailboxDisplay(ctx, next); err != nil {
		return nil, fault.From(err, "database_unavailable")
	}
	b.config = next
	b.publish()
	return directory, nil
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
func (m *Manager) DeliveryView() settings.DeliveryView {
	m.deliveryMu.RLock()
	defer m.deliveryMu.RUnlock()
	view := m.delivery.View()
	view.NativeAvailable = m.native.Available()
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
	return m.testDelivery(ctx, next)
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
	return m.testDelivery(ctx, current)
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
	m.deliveryMu.Lock()
	defer m.deliveryMu.Unlock()
	if err := m.ctx.Err(); err != nil {
		return err
	}
	if m.delivery.Revision != current.Revision {
		return fault.New("settings_conflict")
	}
	if next.Channel == "native" {
		target, err := m.store.NativePairing(ctx, next.NativePairingID)
		if err != nil {
			if fault.From(err, "database_unavailable").Code == "native_pairing_not_found" {
				return fault.New("native_target_unavailable")
			}
			return err
		}
		if target.State != "active" {
			return fault.New("native_target_unavailable")
		}
	}
	if err := m.settings.SaveDelivery(ctx, current, next, current.Revision); err != nil {
		return err
	}
	next.Revision = current.Revision + 1
	m.applyDelivery(next)
	return nil
}

// applyDelivery publishes a committed configuration while deliveryMu is held.
func (m *Manager) applyDelivery(next settings.Delivery) {
	m.delivery = next
	m.deliveryInvalid = false
	// Membership and code detection change together: a newly added mailbox
	// either appears in this snapshot or starts with the new detection setting.
	m.mu.Lock()
	m.detectCodes.Store(next.Channel == "native")
	boxes := m.snapshotLocked()
	m.mu.Unlock()
	for _, b := range boxes {
		if state := b.state.Load(); state != nil {
			state.monitor.SetDetectCodes(m.detectCodes.Load())
		}
	}
	m.dispatcher.Configure(m.deliverySender(next), next.RetryCount, m.delivery.RecordsPreview())
	m.log.Info("Delivery settings updated", "mailbox_id", "", "channel", next.Channel, "revision", next.Revision)
}

func (m *Manager) revokeNativePairing(ctx context.Context, id string, unlink bool) error {
	m.deliveryMu.Lock()
	defer m.deliveryMu.Unlock()
	next, err := m.settings.RevokeNativePairing(ctx, id, unlink, m.delivery)
	if err != nil {
		return err
	}
	if next.Revision != m.delivery.Revision {
		m.applyDelivery(next)
	}
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

func (m *Manager) deliverySender(d settings.Delivery) delivery.Sender {
	if d.Channel == "" {
		return nil
	}
	if d.Channel == "native" {
		return m.native.ForPairing(d.NativePairingID)
	}
	return m.newSender(d)
}

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
	m.applyDelivery(next)
	return nil
}
