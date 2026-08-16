package relay

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zesuy/mcastferry/internal/config"
	"github.com/zesuy/mcastferry/internal/session"
)

type source struct {
	packets chan []byte
	closed  chan struct{}
	once    sync.Once
}

func newSource() *source {
	return &source{packets: make(chan []byte, 8), closed: make(chan struct{})}
}

func (s *source) Read(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closed:
		return nil, io.EOF
	case payload := <-s.packets:
		return payload, nil
	}
}

func (s *source) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

type addressedConn struct {
	net.Conn
	remote net.Addr
}

func (c addressedConn) RemoteAddr() net.Addr { return c.remote }

func testServer(t *testing.T) (*Server, *source) {
	t.Helper()
	upstream := newSource()
	manager, err := session.NewManager(func(session.Key) (session.Source, error) { return upstream, nil }, session.Limits{
		MaxSessions: 5, MaxClients: 5, MaxClientsPerSession: 5, MaxClientsPerIP: 5,
	}, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.CloseAll)
	return &Server{Config: config.Config{
		InputDevice: "iptv0", InputIfIndex: 7, HTTPListen: netip.MustParseAddrPort("192.168.4.1:4022"),
		AllowedGroups:  []netip.Prefix{netip.MustParsePrefix("239.0.0.0/8")},
		AllowedClients: []netip.Prefix{netip.MustParsePrefix("192.168.4.0/24")},
		AllowedPorts:   []config.PortRange{{From: 1000, To: 9000}},
		MaxSessions:    5, MaxClients: 5, MaxClientsPerSession: 5, MaxClientsPerIP: 5,
		MaxQueueBytes: 4096, ClientWriteTimeout: time.Second, SessionIdleGrace: time.Millisecond,
		LogLevel: "info", LogFormat: "text",
	}, Manager: manager, Version: "0.1.0"}, upstream
}

func connectionPair() (net.Conn, net.Conn) {
	server, client := net.Pipe()
	return addressedConn{Conn: server, remote: &net.TCPAddr{IP: net.ParseIP("192.168.4.22"), Port: 50000}}, client
}

func TestServeConnStreamsCloseDelimitedHTTP(t *testing.T) {
	relay, upstream := testServer(t)
	serverConn, clientConn := connectionPair()
	done := make(chan error, 1)
	go func() { done <- relay.ServeConn(serverConn) }()
	_, _ = io.WriteString(clientConn, "GET /udp/239.1.2.3:7980/ HTTP/1.1\r\nHost: router\r\n\r\n")
	reader := bufio.NewReader(clientConn)
	var headers strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		headers.WriteString(line)
		if line == "\r\n" {
			break
		}
	}
	upstream.packets <- []byte("first-media")
	media := make([]byte, len("first-media"))
	if _, err := io.ReadFull(reader, media); err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(headers.String())
	if !strings.HasPrefix(headers.String(), "HTTP/1.1 200 OK") || strings.Contains(lower, "content-length") || strings.Contains(lower, "transfer-encoding") || string(media) != "first-media" {
		t.Fatalf("headers=%q media=%q", headers.String(), media)
	}
	_ = clientConn.Close()
	upstream.packets <- []byte("wake-writer")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ServeConn did not observe client close")
	}
}

func TestServeConnRejectsBeforeStreaming(t *testing.T) {
	relay, _ := testServer(t)
	for _, request := range []string{
		"POST /udp/239.1.2.3:7980/ HTTP/1.1\r\nHost: x\r\n\r\n",
		"GET /udp/10.0.0.1:7980/ HTTP/1.1\r\nHost: x\r\n\r\n",
		"GET /udp/239.1.2.3:9999/ HTTP/1.1\r\nHost: x\r\n\r\n",
		"GET /udp/239.1.2.3:7980/extra HTTP/1.1\r\nHost: x\r\n\r\n",
	} {
		serverConn, clientConn := connectionPair()
		done := make(chan error, 1)
		go func() { done <- relay.ServeConn(serverConn) }()
		_, _ = io.WriteString(clientConn, request)
		response, err := io.ReadAll(clientConn)
		_ = clientConn.Close()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(response), "200 OK") || !strings.Contains(string(response), "Content-Length: 0") {
			t.Fatalf("request %q response %q", request, response)
		}
		<-done
	}
}

func TestStatusIsOrdinaryBoundedResponse(t *testing.T) {
	relay, _ := testServer(t)
	serverConn, clientConn := connectionPair()
	done := make(chan error, 1)
	go func() { done <- relay.ServeConn(serverConn) }()
	_, _ = io.WriteString(clientConn, "GET /status HTTP/1.1\r\nHost: router\r\n\r\n")
	response, err := io.ReadAll(clientConn)
	_ = clientConn.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(response), "Content-Type: application/json") || !strings.HasSuffix(string(response), "{\"sessions\":[]}") {
		t.Fatalf("unexpected status response %q", response)
	}
}
