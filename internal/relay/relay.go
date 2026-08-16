// Package relay parses bounded HTTP/1.x requests and attaches them to shared sessions.
package relay

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/zesuy/mcastferry/internal/config"
	"github.com/zesuy/mcastferry/internal/httpstream"
	"github.com/zesuy/mcastferry/internal/rtp"
	"github.com/zesuy/mcastferry/internal/session"
	"github.com/zesuy/mcastferry/internal/urlx"
)

const (
	requestLineLimit  = 4 << 10
	headerBytesLimit  = 16 << 10
	headerCountLimit  = 64
	headerReadTimeout = 5 * time.Second
)

type Server struct {
	Config  config.Config
	Manager *session.Manager
	Version string
}

func (s *Server) ServeConn(conn net.Conn) error {
	defer conn.Close()
	if s.Manager == nil {
		return errors.New("session manager is required")
	}
	if tcp, ok := conn.(interface{ SetNoDelay(bool) error }); ok {
		_ = tcp.SetNoDelay(true)
	}
	_ = conn.SetReadDeadline(time.Now().Add(headerReadTimeout))
	request, err := readRequest(bufio.NewReaderSize(conn, headerBytesLimit+1))
	if err != nil {
		writeError(conn, "HTTP/1.1", 400, "Bad Request")
		return err
	}
	peer, err := peerAddress(conn.RemoteAddr())
	if err != nil || !s.Config.AllowsClient(peer) {
		writeError(conn, request.version, 403, "Forbidden")
		return errors.New("client is not allowed")
	}
	if request.path == "/status" {
		if request.method != "GET" {
			writeError(conn, request.version, 405, "Method Not Allowed")
			return errors.New("status route requires GET")
		}
		return writeStatus(conn, request.version, s.Manager.Snapshots())
	}
	if request.method != "GET" {
		writeError(conn, request.version, 405, "Method Not Allowed")
		return errors.New("live route requires GET")
	}
	target, err := urlx.Parse(request.path)
	if err != nil {
		writeError(conn, request.version, 400, "Bad Request")
		return err
	}
	if !s.Config.AllowsGroup(target.Group) || !s.Config.AllowsPort(target.Port) {
		writeError(conn, request.version, 403, "Forbidden")
		return errors.New("stream target is not allowed")
	}
	_ = conn.SetReadDeadline(time.Time{})
	streamSession, err := s.Manager.Get(context.Background(), session.Key{
		IfIndex: s.Config.InputIfIndex, Group: target.Group, Port: target.Port, Intent: rtp.ModeAuto,
	})
	if err != nil {
		writeError(conn, request.version, 503, "Service Unavailable")
		return err
	}
	client, err := s.Manager.Attach(streamSession, peer, s.Config.MaxQueueBytes, 3)
	if err != nil {
		s.Manager.DiscardIfEmpty(streamSession)
		writeError(conn, request.version, 503, "Service Unavailable")
		return err
	}
	defer s.Manager.Detach(streamSession, client)
	version := s.Version
	if version == "" {
		version = "dev"
	}
	return httpstream.Serve(context.Background(), conn, request.version, "mcastferry/"+version, client, s.Config.ClientWriteTimeout)
}

type statusResponse struct {
	Sessions []statusSession `json:"sessions"`
}

type statusSession struct {
	InterfaceIndex int    `json:"interface_index"`
	Group          string `json:"group"`
	Port           uint16 `json:"port"`
	Mode           string `json:"mode"`
	Clients        int    `json:"clients"`
	Packets        uint64 `json:"packets"`
	Bytes          uint64 `json:"bytes"`
	LastPacket     string `json:"last_packet,omitempty"`
	InvalidRTP     uint64 `json:"rtp_invalid"`
	SequenceGaps   uint64 `json:"rtp_sequence_gaps"`
	SSRCChanges    uint64 `json:"rtp_ssrc_changes"`
}

func writeStatus(conn net.Conn, version string, snapshots []session.Snapshot) error {
	response := statusResponse{Sessions: make([]statusSession, 0, len(snapshots))}
	for _, snapshot := range snapshots {
		lastPacket := ""
		if !snapshot.LastPacket.IsZero() {
			lastPacket = snapshot.LastPacket.UTC().Format(time.RFC3339Nano)
		}
		response.Sessions = append(response.Sessions, statusSession{
			InterfaceIndex: snapshot.Key.IfIndex, Group: snapshot.Key.Group.String(), Port: snapshot.Key.Port,
			Mode: snapshot.Mode.String(), Clients: snapshot.Clients, Packets: snapshot.Packets, Bytes: snapshot.Bytes,
			LastPacket: lastPacket, InvalidRTP: snapshot.InvalidRTP,
			SequenceGaps: snapshot.SequenceGaps, SSRCChanges: snapshot.SSRCChanges,
		})
	}
	body, err := json.Marshal(response)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("%s 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", version, len(body))
	if _, err := io.WriteString(conn, header); err != nil {
		return err
	}
	for len(body) > 0 {
		n, writeErr := conn.Write(body)
		if writeErr != nil {
			return writeErr
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		body = body[n:]
	}
	return nil
}

type request struct {
	method  string
	path    string
	version string
}

func readRequest(reader *bufio.Reader) (request, error) {
	line, consumed, err := readLine(reader, requestLineLimit)
	if err != nil {
		return request{}, err
	}
	parts := strings.Split(line, " ")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] != "HTTP/1.0" && parts[2] != "HTTP/1.1" {
		return request{}, errors.New("invalid request line")
	}
	for headers := 0; ; headers++ {
		if headers >= headerCountLimit {
			return request{}, errors.New("too many request headers")
		}
		header, n, err := readLine(reader, headerBytesLimit-consumed)
		consumed += n
		if err != nil || consumed > headerBytesLimit {
			return request{}, errors.New("invalid or oversized request headers")
		}
		if header == "" {
			break
		}
		name, _, found := strings.Cut(header, ":")
		if !found || name == "" || strings.TrimSpace(name) != name {
			return request{}, errors.New("malformed request header")
		}
	}
	return request{method: parts[0], path: parts[1], version: parts[2]}, nil
}

func readLine(reader *bufio.Reader, limit int) (string, int, error) {
	if limit <= 0 {
		return "", 0, errors.New("line exceeds limit")
	}
	data, err := reader.ReadSlice('\n')
	if err != nil {
		return "", len(data), err
	}
	line := string(data)
	if len(line) > limit || len(line) < 2 || !strings.HasSuffix(line, "\r\n") {
		return "", len(line), errors.New("line exceeds limit or is not CRLF terminated")
	}
	return strings.TrimSuffix(line, "\r\n"), len(line), nil
}

func peerAddress(address net.Addr) (netip.Addr, error) {
	if tcp, ok := address.(*net.TCPAddr); ok {
		return tcp.AddrPort().Addr().Unmap(), nil
	}
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return netip.Addr{}, err
	}
	parsed, err := netip.ParseAddr(host)
	return parsed.Unmap(), err
}

func writeError(conn net.Conn, version string, status int, reason string) {
	if version != "HTTP/1.0" && version != "HTTP/1.1" {
		version = "HTTP/1.1"
	}
	response := fmt.Sprintf("%s %d %s\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", version, status, reason)
	_, _ = io.WriteString(conn, response)
}
