package session

import (
	"context"
	"errors"
	"io"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zesuy/mcastferry/internal/rtp"
)

type fakeSource struct {
	packets chan []byte
	closed  chan struct{}
	once    sync.Once
}

func newFakeSource() *fakeSource {
	return &fakeSource{packets: make(chan []byte, 32), closed: make(chan struct{})}
}

func (s *fakeSource) Read(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closed:
		return nil, io.EOF
	case packet := <-s.packets:
		return packet, nil
	}
}

func (s *fakeSource) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

func testKey(last byte) Key {
	return Key{IfIndex: 7, Group: netip.AddrFrom4([4]byte{239, 1, 2, last}), Port: 7980, Intent: rtp.ModeAuto}
}

func testLimits() Limits {
	return Limits{MaxSessions: 5, MaxClients: 5, MaxClientsPerSession: 5, MaxClientsPerIP: 5}
}

func newTestManager(t *testing.T, factory Factory, limits Limits, idle time.Duration) *Manager {
	t.Helper()
	m, err := NewManager(factory, limits, idle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.CloseAll)
	return m
}

func readWithTimeout(t *testing.T, client *Client) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return client.Read(ctx)
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met")
}

func TestConcurrentGetSharesSource(t *testing.T) {
	var creations atomic.Int32
	source := newFakeSource()
	m := newTestManager(t, func(Key) (Source, error) {
		creations.Add(1)
		return source, nil
	}, testLimits(), time.Second)
	const callers = 12
	results := make(chan *Session, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := m.Get(context.Background(), testKey(3))
			if err != nil {
				t.Errorf("Get: %v", err)
				return
			}
			results <- s
		}()
	}
	wg.Wait()
	close(results)
	var first *Session
	for s := range results {
		if first == nil {
			first = s
		} else if first != s {
			t.Fatal("same key produced distinct sessions")
		}
	}
	if creations.Load() != 1 {
		t.Fatalf("factory called %d times", creations.Load())
	}
}

func TestFanoutAndIsolation(t *testing.T) {
	sources := map[byte]*fakeSource{3: newFakeSource(), 4: newFakeSource()}
	m := newTestManager(t, func(key Key) (Source, error) {
		group := key.Group.As4()
		return sources[group[3]], nil
	}, testLimits(), time.Second)
	s1, _ := m.Get(context.Background(), testKey(3))
	s2, _ := m.Get(context.Background(), testKey(4))
	peer := netip.MustParseAddr("192.0.2.1")
	c1, _ := m.Attach(s1, peer, 1024, 3)
	c2, _ := m.Attach(s1, netip.MustParseAddr("192.0.2.2"), 1024, 3)
	c3, _ := m.Attach(s2, netip.MustParseAddr("192.0.2.3"), 1024, 3)
	sources[3].packets <- []byte("channel-three")
	sources[4].packets <- []byte("channel-four")
	for _, client := range []*Client{c1, c2} {
		payload, err := readWithTimeout(t, client)
		if err != nil || string(payload) != "channel-three" {
			t.Fatalf("fanout payload=%q err=%v", payload, err)
		}
	}
	payload, err := readWithTimeout(t, c3)
	if err != nil || string(payload) != "channel-four" {
		t.Fatalf("isolated payload=%q err=%v", payload, err)
	}
}

func TestSlowClientIsRemovedWithoutBlockingFastClient(t *testing.T) {
	source := newFakeSource()
	m := newTestManager(t, func(Key) (Source, error) { return source, nil }, testLimits(), time.Second)
	s, _ := m.Get(context.Background(), testKey(3))
	slow, _ := m.Attach(s, netip.MustParseAddr("192.0.2.1"), 4, 2)
	fast, _ := m.Attach(s, netip.MustParseAddr("192.0.2.2"), 64, 2)
	for i := 0; i < 3; i++ {
		source.packets <- []byte("data")
	}
	waitFor(t, func() bool { return m.ClientCount() == 1 })
	for i := 0; i < 3; i++ {
		payload, err := readWithTimeout(t, fast)
		if err != nil || string(payload) != "data" {
			t.Fatalf("fast client payload=%q err=%v", payload, err)
		}
	}
	if _, err := readWithTimeout(t, slow); err != nil {
		t.Fatalf("queued slow payload should be readable: %v", err)
	}
	if _, err := readWithTimeout(t, slow); !errors.Is(err, ErrSlowClient) {
		t.Fatalf("expected slow client error, got %v", err)
	}
}

func TestLimitsDetachAndIdleRelease(t *testing.T) {
	source := newFakeSource()
	limits := Limits{MaxSessions: 1, MaxClients: 1, MaxClientsPerSession: 1, MaxClientsPerIP: 1}
	m := newTestManager(t, func(Key) (Source, error) { return source, nil }, limits, 5*time.Millisecond)
	s, _ := m.Get(context.Background(), testKey(3))
	if _, err := m.Get(context.Background(), testKey(4)); !errors.Is(err, ErrLimit) {
		t.Fatalf("expected session limit, got %v", err)
	}
	client, err := m.Attach(s, netip.MustParseAddr("192.0.2.1"), 1024, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(s, netip.MustParseAddr("192.0.2.2"), 1024, 3); !errors.Is(err, ErrLimit) {
		t.Fatalf("expected client limit, got %v", err)
	}
	m.Detach(s, client)
	if m.ClientCount() != 0 {
		t.Fatal("client reservation was not released")
	}
	waitFor(t, func() bool { return m.SessionCount() == 0 })
}

func TestCloseAllReleasesClients(t *testing.T) {
	source := newFakeSource()
	m := newTestManager(t, func(Key) (Source, error) { return source, nil }, testLimits(), time.Second)
	s, _ := m.Get(context.Background(), testKey(3))
	client, _ := m.Attach(s, netip.MustParseAddr("192.0.2.1"), 1024, 3)
	m.CloseAll()
	waitFor(t, func() bool { return m.SessionCount() == 0 && m.ClientCount() == 0 })
	if _, err := readWithTimeout(t, client); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("expected manager closed, got %v", err)
	}
}
