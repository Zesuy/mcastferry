// Package config defines the resolved, platform-neutral runtime configuration.
package config

import (
	"errors"
	"flag"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultMaxSessions          = 5
	DefaultMaxClients           = 5
	DefaultMaxClientsPerSession = 5
	DefaultMaxClientsPerIP      = 5
	DefaultMaxQueueBytes        = 512 << 10
	DefaultClientWriteTimeout   = 2 * time.Second
	DefaultSessionIdleGrace     = time.Second

	HardMaxSessions          = 8
	HardMaxClients           = 16
	HardMaxClientsPerSession = 16
	HardMaxClientsPerIP      = 16
	HardMaxQueueBytes        = 2 << 20
	HardMaxWriteTimeout      = 5 * time.Second
	HardMaxIdleGrace         = 3 * time.Second
)

type PortRange struct {
	From uint16
	To   uint16
}

func (r PortRange) Contains(port uint16) bool {
	return r.From <= port && port <= r.To
}

type Config struct {
	InputDevice          string
	InputIfIndex         int
	HTTPListen           netip.AddrPort
	AllowedGroups        []netip.Prefix
	AllowedClients       []netip.Prefix
	AllowedPorts         []PortRange
	MaxSessions          int
	MaxClients           int
	MaxClientsPerSession int
	MaxClientsPerIP      int
	MaxQueueBytes        int
	ClientWriteTimeout   time.Duration
	SessionIdleGrace     time.Duration
	LogLevel             string
	LogFormat            string
}

func (c Config) Validate() error {
	if c.InputDevice == "" || c.InputIfIndex <= 0 {
		return errors.New("resolved multicast input device and interface index are required")
	}
	if !c.HTTPListen.IsValid() || !c.HTTPListen.Addr().Is4() || c.HTTPListen.Addr().IsUnspecified() {
		return errors.New("explicit non-wildcard IPv4 HTTP listen address is required")
	}
	if len(c.AllowedGroups) == 0 || len(c.AllowedClients) == 0 || len(c.AllowedPorts) == 0 {
		return errors.New("group, client, and port allowlists are required")
	}
	for _, prefix := range c.AllowedGroups {
		if !prefix.IsValid() || !prefix.Addr().Is4() || !prefix.Addr().IsMulticast() || prefix.Bits() < 4 {
			return fmt.Errorf("group allowlist %q is not contained in IPv4 multicast space", prefix)
		}
	}
	for _, prefix := range c.AllowedClients {
		if !prefix.IsValid() || !prefix.Addr().Is4() {
			return fmt.Errorf("client allowlist %q is not a valid IPv4 prefix", prefix)
		}
	}
	for _, ports := range c.AllowedPorts {
		if ports.From == 0 || ports.To == 0 || ports.From > ports.To {
			return fmt.Errorf("invalid allowed port range %d-%d", ports.From, ports.To)
		}
	}
	if c.MaxSessions < 1 || c.MaxSessions > HardMaxSessions {
		return fmt.Errorf("max-sessions must be between 1 and %d", HardMaxSessions)
	}
	if c.MaxClients < 1 || c.MaxClients > HardMaxClients {
		return fmt.Errorf("max-clients must be between 1 and %d", HardMaxClients)
	}
	if c.MaxClientsPerSession < 1 || c.MaxClientsPerSession > HardMaxClientsPerSession {
		return fmt.Errorf("max-clients-per-session must be between 1 and %d", HardMaxClientsPerSession)
	}
	if c.MaxClientsPerIP < 1 || c.MaxClientsPerIP > HardMaxClientsPerIP {
		return fmt.Errorf("max-clients-per-ip must be between 1 and %d", HardMaxClientsPerIP)
	}
	if c.MaxQueueBytes < 1 || c.MaxQueueBytes > HardMaxQueueBytes {
		return fmt.Errorf("max-queue-bytes must be between 1 and %d", HardMaxQueueBytes)
	}
	if c.ClientWriteTimeout <= 0 || c.ClientWriteTimeout > HardMaxWriteTimeout {
		return fmt.Errorf("client-write-timeout must be positive and at most %s", HardMaxWriteTimeout)
	}
	if c.SessionIdleGrace < 0 || c.SessionIdleGrace > HardMaxIdleGrace {
		return fmt.Errorf("session-idle-grace must be between zero and %s", HardMaxIdleGrace)
	}
	if c.LogLevel != "debug" && c.LogLevel != "info" && c.LogLevel != "warn" && c.LogLevel != "error" {
		return fmt.Errorf("unsupported log-level %q", c.LogLevel)
	}
	if c.LogFormat != "text" && c.LogFormat != "json" {
		return fmt.Errorf("unsupported log-format %q", c.LogFormat)
	}
	return nil
}

func (c Config) AllowsGroup(group netip.Addr) bool {
	for _, prefix := range c.AllowedGroups {
		if prefix.Contains(group) {
			return true
		}
	}
	return false
}

func (c Config) AllowsClient(client netip.Addr) bool {
	for _, prefix := range c.AllowedClients {
		if prefix.Contains(client) {
			return true
		}
	}
	return false
}

func (c Config) AllowsPort(port uint16) bool {
	for _, ports := range c.AllowedPorts {
		if ports.Contains(port) {
			return true
		}
	}
	return false
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("allowlist value cannot be empty")
	}
	*s = append(*s, value)
	return nil
}

type Options struct {
	inputDevice          string
	httpListen           string
	allowedGroups        stringList
	allowedClients       stringList
	allowedPorts         stringList
	maxSessions          int
	maxClients           int
	maxClientsPerSession int
	maxClientsPerIP      int
	maxQueueBytes        int
	clientWriteTimeout   time.Duration
	sessionIdleGrace     time.Duration
	logLevel             string
	logFormat            string
}

func BindFlags(fs *flag.FlagSet) *Options {
	o := &Options{}
	fs.StringVar(&o.inputDevice, "multicast-input", "", "resolved multicast input device")
	fs.StringVar(&o.httpListen, "http-listen", "", "explicit IPv4 HTTP listen address")
	fs.Var(&o.allowedGroups, "allowed-group", "allowed IPv4 multicast CIDR (repeatable)")
	fs.Var(&o.allowedClients, "allowed-client", "allowed IPv4 client CIDR (repeatable)")
	fs.Var(&o.allowedPorts, "allowed-port", "allowed UDP port or inclusive range (repeatable)")
	fs.IntVar(&o.maxSessions, "max-sessions", DefaultMaxSessions, "maximum active multicast sessions")
	fs.IntVar(&o.maxClients, "max-clients", DefaultMaxClients, "maximum concurrent HTTP clients")
	fs.IntVar(&o.maxClientsPerSession, "max-clients-per-session", DefaultMaxClientsPerSession, "maximum clients per session")
	fs.IntVar(&o.maxClientsPerIP, "max-clients-per-ip", DefaultMaxClientsPerIP, "maximum clients per source IP")
	fs.IntVar(&o.maxQueueBytes, "max-queue-bytes", DefaultMaxQueueBytes, "per-client queue byte limit")
	fs.DurationVar(&o.clientWriteTimeout, "client-write-timeout", DefaultClientWriteTimeout, "deadline for each client write")
	fs.DurationVar(&o.sessionIdleGrace, "session-idle-grace", DefaultSessionIdleGrace, "delay before closing an unused session")
	fs.StringVar(&o.logLevel, "log-level", "info", "log level: debug, info, warn, or error")
	fs.StringVar(&o.logFormat, "log-format", "text", "log format: text or json")
	return o
}

type InterfaceLookup func(name string) (int, error)

func (o *Options) Resolve(lookup InterfaceLookup) (Config, error) {
	if strings.TrimSpace(o.inputDevice) == "" || strings.TrimSpace(o.httpListen) == "" {
		return Config{}, errors.New("multicast-input and http-listen are required")
	}
	ifIndex, err := lookup(o.inputDevice)
	if err != nil {
		return Config{}, fmt.Errorf("resolve multicast input %q: %w", o.inputDevice, err)
	}
	listen, err := netip.ParseAddrPort(o.httpListen)
	if err != nil {
		return Config{}, fmt.Errorf("parse http-listen: %w", err)
	}
	groups, err := parsePrefixes(o.allowedGroups)
	if err != nil {
		return Config{}, fmt.Errorf("parse allowed-group: %w", err)
	}
	clients, err := parsePrefixes(o.allowedClients)
	if err != nil {
		return Config{}, fmt.Errorf("parse allowed-client: %w", err)
	}
	ports, err := parsePortRanges(o.allowedPorts)
	if err != nil {
		return Config{}, fmt.Errorf("parse allowed-port: %w", err)
	}
	cfg := Config{
		InputDevice: o.inputDevice, InputIfIndex: ifIndex, HTTPListen: listen,
		AllowedGroups: groups, AllowedClients: clients, AllowedPorts: ports,
		MaxSessions: o.maxSessions, MaxClients: o.maxClients,
		MaxClientsPerSession: o.maxClientsPerSession, MaxClientsPerIP: o.maxClientsPerIP,
		MaxQueueBytes: o.maxQueueBytes, ClientWriteTimeout: o.clientWriteTimeout,
		SessionIdleGrace: o.sessionIdleGrace, LogLevel: o.logLevel, LogFormat: o.logFormat,
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func parsePrefixes(values []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func parsePortRanges(values []string) ([]PortRange, error) {
	ranges := make([]PortRange, 0, len(values))
	for _, value := range values {
		fromText, toText, found := strings.Cut(value, "-")
		if !found {
			toText = fromText
		}
		from, err := strconv.ParseUint(fromText, 10, 16)
		if err != nil || from == 0 {
			return nil, fmt.Errorf("invalid port %q", fromText)
		}
		to, err := strconv.ParseUint(toText, 10, 16)
		if err != nil || to == 0 || from > to {
			return nil, fmt.Errorf("invalid port range %q", value)
		}
		ranges = append(ranges, PortRange{From: uint16(from), To: uint16(to)})
	}
	return ranges, nil
}
