package httpstream

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

type payloadReader struct {
	payloads [][]byte
	index    int
}

func (r *payloadReader) Read(context.Context) ([]byte, error) {
	if r.index >= len(r.payloads) {
		return nil, io.EOF
	}
	payload := r.payloads[r.index]
	r.index++
	return payload, nil
}

func TestServeWritesCloseDelimitedWireFormat(t *testing.T) {
	for _, version := range []string{"HTTP/1.0", "HTTP/1.1"} {
		server, client := net.Pipe()
		done := make(chan error, 1)
		go func() {
			done <- Serve(context.Background(), server, version, "mcastferry/0.1.0", &payloadReader{payloads: [][]byte{[]byte("abc"), []byte("def")}}, time.Second)
			_ = server.Close()
		}()
		wire, err := io.ReadAll(client)
		_ = client.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := <-done; !errors.Is(err, io.EOF) {
			t.Fatalf("Serve returned %v", err)
		}
		parts := bytes.SplitN(wire, []byte("\r\n\r\n"), 2)
		if len(parts) != 2 || string(parts[1]) != "abcdef" {
			t.Fatalf("unexpected wire bytes %q", wire)
		}
		headers := strings.ToLower(string(parts[0]))
		if !strings.HasPrefix(headers, strings.ToLower(version)+" 200 ok") || strings.Contains(headers, "content-length") || strings.Contains(headers, "transfer-encoding") {
			t.Fatalf("unexpected response headers %q", parts[0])
		}
	}
}

func TestServeNeverWritesTextAfterStreamingError(t *testing.T) {
	server, client := net.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- Serve(context.Background(), server, "HTTP/1.1", "mcastferry/dev", &payloadReader{payloads: [][]byte{[]byte("media")}}, time.Second)
		_ = server.Close()
	}()
	reader := bufio.NewReader(client)
	wire, err := io.ReadAll(reader)
	_ = client.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, io.EOF) {
		t.Fatalf("Serve returned %v", err)
	}
	if strings.Contains(string(wire), "EOF") || !strings.HasSuffix(string(wire), "media") {
		t.Fatalf("stream error leaked into body: %q", wire)
	}
}
