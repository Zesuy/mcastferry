// Package httpstream writes udpxy-compatible close-delimited HTTP streams.
package httpstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

type PayloadReader interface {
	Read(context.Context) ([]byte, error)
}

func Serve(ctx context.Context, conn net.Conn, httpVersion, serverName string, reader PayloadReader, writeTimeout time.Duration) error {
	if httpVersion != "HTTP/1.0" && httpVersion != "HTTP/1.1" {
		return errors.New("unsupported HTTP version")
	}
	if strings.ContainsAny(serverName, "\r\n") || serverName == "" {
		return errors.New("invalid Server header")
	}
	header := fmt.Sprintf("%s 200 OK\r\nServer: %s\r\nContent-Type: application/octet-stream\r\nConnection: close\r\n\r\n", httpVersion, serverName)
	if err := writeAll(conn, []byte(header), writeTimeout); err != nil {
		return err
	}
	for {
		payload, err := reader.Read(ctx)
		if err != nil {
			return err
		}
		if len(payload) == 0 {
			continue
		}
		if err := writeAll(conn, payload, writeTimeout); err != nil {
			return err
		}
	}
}

func writeAll(conn net.Conn, data []byte, timeout time.Duration) error {
	if timeout > 0 {
		if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
			return err
		}
	}
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
