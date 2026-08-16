// Package session shares multicast sources between clients and provides bounded fan-out.
package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"sync"
	"time"

	"github.com/zesuy/mcastferry/internal/rtp"
)

var (
	ErrLimit         = errors.New("session resource limit reached")
	ErrManagerClosed = errors.New("session manager closed")
	ErrSessionClosed = errors.New("session closed")
	ErrSlowClient    = errors.New("client queue remained full")
)

type Source interface {
	Read(context.Context) ([]byte, error)
	Close() error
}

type Factory func(Key) (Source, error)

type Key struct {
	IfIndex int
	Group   netip.Addr
	Port    uint16
	Intent  rtp.Mode
}

func (k Key) Validate() error {
	if k.IfIndex <= 0 || !k.Group.Is4() || !k.Group.IsMulticast() || k.Port == 0 {
		return errors.New("invalid multicast session key")
	}
	if k.Intent != rtp.ModeAuto {
		return errors.New("v0.1 session intent must be auto")
	}
	return nil
}

type Limits struct {
	MaxSessions          int
	MaxClients           int
	MaxClientsPerSession int
	MaxClientsPerIP      int
}

func (l Limits) Validate() error {
	if l.MaxSessions <= 0 || l.MaxClients <= 0 || l.MaxClientsPerSession <= 0 || l.MaxClientsPerIP <= 0 {
		return errors.New("session and client limits must be positive")
	}
	return nil
}

type pendingCreation struct {
	done    chan struct{}
	session *Session
	err     error
}

type Manager struct {
	factory   Factory
	limits    Limits
	idleGrace time.Duration

	mu       sync.Mutex
	sessions map[Key]*Session
	pending  map[Key]*pendingCreation
	closed   bool

	clientMu     sync.Mutex
	totalClients int
	clientsByIP  map[netip.Addr]int
}

func NewManager(factory Factory, limits Limits, idleGrace time.Duration) (*Manager, error) {
	if factory == nil {
		return nil, errors.New("session source factory is required")
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if idleGrace < 0 {
		return nil, errors.New("idle grace cannot be negative")
	}
	return &Manager{
		factory: factory, limits: limits, idleGrace: idleGrace,
		sessions: make(map[Key]*Session), pending: make(map[Key]*pendingCreation),
		clientsByIP: make(map[netip.Addr]int),
	}, nil
}

func (m *Manager) Get(ctx context.Context, key Key) (*Session, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}
	for {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return nil, ErrManagerClosed
		}
		if existing := m.sessions[key]; existing != nil {
			m.mu.Unlock()
			if !existing.isClosed() {
				return existing, nil
			}
			m.removeSession(existing)
			continue
		}
		if pending := m.pending[key]; pending != nil {
			m.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-pending.done:
				return pending.session, pending.err
			}
		}
		if len(m.sessions)+len(m.pending) >= m.limits.MaxSessions {
			m.mu.Unlock()
			return nil, fmt.Errorf("%w: maximum sessions", ErrLimit)
		}
		pending := &pendingCreation{done: make(chan struct{})}
		m.pending[key] = pending
		m.mu.Unlock()

		source, err := m.factory(key)
		var created *Session
		if err == nil {
			created = newSession(m, key, source)
		}

		m.mu.Lock()
		if m.closed && err == nil {
			err = ErrManagerClosed
			_ = source.Close()
			created = nil
		}
		if err == nil {
			m.sessions[key] = created
			created.start()
		}
		delete(m.pending, key)
		pending.session, pending.err = created, err
		close(pending.done)
		m.mu.Unlock()
		return created, err
	}
}

func (m *Manager) Attach(s *Session, peer netip.Addr, maxQueueBytes, slowThreshold int) (*Client, error) {
	if s == nil || !peer.IsValid() || !peer.Is4() || maxQueueBytes <= 0 || slowThreshold <= 0 {
		return nil, errors.New("invalid client attachment")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrSessionClosed
	}
	if len(s.clients) >= m.limits.MaxClientsPerSession {
		return nil, fmt.Errorf("%w: maximum clients per session", ErrLimit)
	}
	m.clientMu.Lock()
	defer m.clientMu.Unlock()
	if m.totalClients >= m.limits.MaxClients || m.clientsByIP[peer] >= m.limits.MaxClientsPerIP {
		return nil, fmt.Errorf("%w: maximum clients", ErrLimit)
	}
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
	client := newClient(peer, maxQueueBytes, slowThreshold)
	s.clients[client] = struct{}{}
	m.totalClients++
	m.clientsByIP[peer]++
	return client, nil
}

func (m *Manager) Detach(s *Session, client *Client) {
	if s == nil || client == nil {
		return
	}
	s.mu.Lock()
	removed := s.removeClientLocked(client, io.EOF)
	empty := !s.closed && len(s.clients) == 0
	if empty {
		s.scheduleIdleLocked()
	}
	s.mu.Unlock()
	if removed {
		m.releaseClients([]*Client{client})
	}
}

func (m *Manager) DiscardIfEmpty(s *Session) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.closed && len(s.clients) == 0 {
		s.scheduleIdleLocked()
	}
	s.mu.Unlock()
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()
	for _, s := range sessions {
		m.terminate(s, ErrManagerClosed)
	}
}

func (m *Manager) SessionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

func (m *Manager) ClientCount() int {
	m.clientMu.Lock()
	defer m.clientMu.Unlock()
	return m.totalClients
}

func (m *Manager) Snapshots() []Snapshot {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()
	result := make([]Snapshot, 0, len(sessions))
	for _, s := range sessions {
		result = append(result, s.Snapshot())
	}
	return result
}

func (m *Manager) removeSession(s *Session) {
	m.mu.Lock()
	if m.sessions[s.key] == s {
		delete(m.sessions, s.key)
	}
	m.mu.Unlock()
}

func (m *Manager) terminate(s *Session, reason error) {
	if !s.markClosed(reason, false) {
		return
	}
	m.removeSession(s)
	clients := s.takeClients()
	m.releaseClients(clients)
	_ = s.source.Close()
	s.cancel()
	close(s.done)
}

func (m *Manager) terminateIfEmpty(s *Session) {
	if !s.markClosed(ErrSessionClosed, true) {
		return
	}
	m.removeSession(s)
	_ = s.source.Close()
	s.cancel()
	close(s.done)
}

func (m *Manager) releaseClients(clients []*Client) {
	if len(clients) == 0 {
		return
	}
	m.clientMu.Lock()
	for _, client := range clients {
		if m.totalClients > 0 {
			m.totalClients--
		}
		if count := m.clientsByIP[client.peer]; count <= 1 {
			delete(m.clientsByIP, client.peer)
		} else {
			m.clientsByIP[client.peer] = count - 1
		}
	}
	m.clientMu.Unlock()
}

type Session struct {
	manager   *Manager
	key       Key
	source    Source
	processor *rtp.Processor
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}

	mu         sync.Mutex
	clients    map[*Client]struct{}
	idleTimer  *time.Timer
	closed     bool
	closeError error
	mode       rtp.Mode
	packets    uint64
	bytes      uint64
	lastPacket time.Time
}

func newSession(manager *Manager, key Key, source Source) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{
		manager: manager, key: key, source: source, processor: rtp.NewProcessor(),
		ctx: ctx, cancel: cancel, done: make(chan struct{}), clients: make(map[*Client]struct{}), mode: rtp.ModeAuto,
	}
}

func (s *Session) start() { go s.readLoop() }

func (s *Session) Key() Key { return s.key }

func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Session) markClosed(reason error, onlyIfEmpty bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || onlyIfEmpty && len(s.clients) != 0 {
		return false
	}
	s.closed = true
	s.closeError = reason
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
	return true
}

func (s *Session) takeClients() []*Client {
	s.mu.Lock()
	clients := make([]*Client, 0, len(s.clients))
	for client := range s.clients {
		client.close(s.closeError)
		clients = append(clients, client)
		delete(s.clients, client)
	}
	s.mu.Unlock()
	return clients
}

func (s *Session) removeClientLocked(client *Client, reason error) bool {
	if _, ok := s.clients[client]; !ok {
		return false
	}
	delete(s.clients, client)
	client.close(reason)
	return true
}

func (s *Session) scheduleIdleLocked() {
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	s.idleTimer = time.AfterFunc(s.manager.idleGrace, func() { s.manager.terminateIfEmpty(s) })
}

func (s *Session) readLoop() {
	for {
		payload, err := s.source.Read(s.ctx)
		if err != nil {
			s.manager.terminate(s, err)
			return
		}
		s.mu.Lock()
		result := s.processor.Process(payload)
		s.packets++
		s.bytes += uint64(len(payload))
		s.lastPacket = time.Now()
		s.mode = result.Mode
		s.mu.Unlock()
		if !result.Valid || len(result.Payload) == 0 {
			continue
		}
		s.broadcast(result.Payload)
	}
}

func (s *Session) broadcast(payload []byte) {
	s.mu.Lock()
	slow := make([]*Client, 0)
	for client := range s.clients {
		if !client.enqueue(payload) {
			delete(s.clients, client)
			slow = append(slow, client)
		}
	}
	empty := !s.closed && len(s.clients) == 0 && len(slow) > 0
	if empty {
		s.scheduleIdleLocked()
	}
	s.mu.Unlock()
	s.manager.releaseClients(slow)
}

type Snapshot struct {
	Key          Key
	Mode         rtp.Mode
	Clients      int
	Packets      uint64
	Bytes        uint64
	LastPacket   time.Time
	InvalidRTP   uint64
	SequenceGaps uint64
	SSRCChanges  uint64
	Closed       bool
}

func (s *Session) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	stats := s.processor.Stats()
	return Snapshot{
		Key: s.key, Mode: s.mode, Clients: len(s.clients), Packets: s.packets, Bytes: s.bytes,
		LastPacket: s.lastPacket, InvalidRTP: stats.InvalidRTP,
		SequenceGaps: stats.SequenceGap, SSRCChanges: stats.SSRCChange, Closed: s.closed,
	}
}

type Client struct {
	peer          netip.Addr
	maxQueueBytes int
	slowThreshold int

	mu          sync.Mutex
	queue       [][]byte
	queuedBytes int
	slowCount   int
	closed      bool
	closeError  error
	notify      chan struct{}
	done        chan struct{}
}

func newClient(peer netip.Addr, maxQueueBytes, slowThreshold int) *Client {
	return &Client{
		peer: peer, maxQueueBytes: maxQueueBytes, slowThreshold: slowThreshold,
		notify: make(chan struct{}, 1), done: make(chan struct{}),
	}
}

func (c *Client) enqueue(payload []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	if len(payload) > c.maxQueueBytes || c.queuedBytes+len(payload) > c.maxQueueBytes {
		c.slowCount++
		if c.slowCount >= c.slowThreshold {
			c.closeLocked(ErrSlowClient)
			return false
		}
		return true
	}
	copyOfPayload := append([]byte(nil), payload...)
	c.queue = append(c.queue, copyOfPayload)
	c.queuedBytes += len(copyOfPayload)
	c.slowCount = 0
	select {
	case c.notify <- struct{}{}:
	default:
	}
	return true
}

func (c *Client) Read(ctx context.Context) ([]byte, error) {
	for {
		c.mu.Lock()
		if len(c.queue) > 0 {
			payload := c.queue[0]
			c.queue[0] = nil
			c.queue = c.queue[1:]
			c.queuedBytes -= len(payload)
			c.mu.Unlock()
			return payload, nil
		}
		if c.closed {
			err := c.closeError
			if err == nil {
				err = io.EOF
			}
			c.mu.Unlock()
			return nil, err
		}
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.done:
		case <-c.notify:
		}
	}
}

func (c *Client) close(reason error) {
	c.mu.Lock()
	c.closeLocked(reason)
	c.mu.Unlock()
}

func (c *Client) closeLocked(reason error) {
	if c.closed {
		return
	}
	c.closed = true
	c.closeError = reason
	close(c.done)
}
