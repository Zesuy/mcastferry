package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/zesuy/mcastferry/internal/config"
	"github.com/zesuy/mcastferry/internal/mcast"
	"github.com/zesuy/mcastferry/internal/relay"
	"github.com/zesuy/mcastferry/internal/session"
)

var version = "dev"

func main() {
	os.Exit(run(os.Stdout, os.Stderr, os.Args[1:]))
}

func run(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("mcastferry", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print version and exit")
	options := config.BindFlags(fs)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mcastferry [options]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "mcastferry %s\n", version)
		return 0
	}
	cfg, err := options.Resolve(func(name string) (int, error) {
		iface, lookupErr := net.InterfaceByName(name)
		if lookupErr != nil {
			return 0, lookupErr
		}
		return iface.Index, nil
	})
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n", err)
		return 2
	}
	if err := configureLogging(cfg.LogLevel, cfg.LogFormat, stderr); err != nil {
		fmt.Fprintf(stderr, "logging error: %v\n", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := serve(ctx, cfg); err != nil {
		slog.Error("server_stopped", "error", err)
		return 1
	}
	return 0
}

type multicastSource struct{ receiver mcast.Receiver }

func (s multicastSource) Read(ctx context.Context) ([]byte, error) {
	for {
		datagram, err := s.receiver.Read(ctx)
		if errors.Is(err, mcast.ErrDatagramRejected) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return datagram.Payload, nil
	}
}

func (s multicastSource) Close() error { return s.receiver.Close() }

func serve(ctx context.Context, cfg config.Config) error {
	manager, err := session.NewManager(func(key session.Key) (session.Source, error) {
		receiver, openErr := mcast.Open(mcast.Config{
			Group: key.Group, Port: key.Port, IfIndex: key.IfIndex, ReceiveBuffer: 4 << 20,
		})
		if openErr != nil {
			return nil, openErr
		}
		slog.Info("session_ready", "group", key.Group, "port", key.Port, "ifindex", key.IfIndex)
		return multicastSource{receiver: receiver}, nil
	}, session.Limits{
		MaxSessions: cfg.MaxSessions, MaxClients: cfg.MaxClients,
		MaxClientsPerSession: cfg.MaxClientsPerSession, MaxClientsPerIP: cfg.MaxClientsPerIP,
	}, cfg.SessionIdleGrace)
	if err != nil {
		return err
	}
	defer manager.CloseAll()

	listener, err := net.Listen("tcp4", cfg.HTTPListen.String())
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPListen, err)
	}
	defer listener.Close()
	slog.Info("server_start", "listen", listener.Addr(), "multicast_input", cfg.InputDevice, "ifindex", cfg.InputIfIndex)

	relayServer := &relay.Server{Config: cfg, Manager: manager, Version: version}
	var connections sync.WaitGroup
	shutdownDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
			manager.CloseAll()
		case <-shutdownDone:
		}
	}()
	defer close(shutdownDone)

	for {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil || errors.Is(acceptErr, net.ErrClosed) {
				break
			}
			return fmt.Errorf("accept HTTP client: %w", acceptErr)
		}
		connections.Add(1)
		go func() {
			defer connections.Done()
			if serveErr := relayServer.ServeConn(conn); serveErr != nil && !errors.Is(serveErr, io.EOF) && !errors.Is(serveErr, net.ErrClosed) {
				slog.Debug("client_detached", "remote", conn.RemoteAddr(), "error", serveErr)
			}
		}()
	}
	connections.Wait()
	slog.Info("server_stopped")
	return nil
}

func configureLogging(levelText, format string, output io.Writer) error {
	var level slog.Level
	switch levelText {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		return fmt.Errorf("unsupported level %q", levelText)
	}
	options := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	switch format {
	case "text":
		handler = slog.NewTextHandler(output, options)
	case "json":
		handler = slog.NewJSONHandler(output, options)
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
	slog.SetDefault(slog.New(handler))
	return nil
}
