package config

import (
	"errors"
	"flag"
	"net/netip"
	"testing"
)

func parseTestConfig(t *testing.T, args ...string) (Config, error) {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opts := BindFlags(fs)
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	return opts.Resolve(func(name string) (int, error) {
		if name != "iptv0" {
			return 0, errors.New("not found")
		}
		return 7, nil
	})
}

func TestResolveAndAllowlists(t *testing.T) {
	cfg, err := parseTestConfig(t,
		"-multicast-input", "iptv0",
		"-http-listen", "192.168.4.1:4022",
		"-allowed-group", "239.0.0.0/8",
		"-allowed-client", "192.168.4.0/24",
		"-allowed-port", "1146-7980",
	)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InputIfIndex != 7 || cfg.MaxSessions != 5 || cfg.MaxClients != 5 {
		t.Fatalf("unexpected resolved config: %+v", cfg)
	}
	if !cfg.AllowsGroup(netip.MustParseAddr("239.1.2.3")) || cfg.AllowsGroup(netip.MustParseAddr("238.1.2.3")) {
		t.Fatal("group allowlist mismatch")
	}
	if !cfg.AllowsClient(netip.MustParseAddr("192.168.4.22")) || cfg.AllowsClient(netip.MustParseAddr("192.168.5.22")) {
		t.Fatal("client allowlist mismatch")
	}
	if !cfg.AllowsPort(7980) || cfg.AllowsPort(9000) {
		t.Fatal("port allowlist mismatch")
	}
}

func TestResolveRejectsMissingAndInvalidValues(t *testing.T) {
	tests := [][]string{
		{},
		{"-multicast-input", "iptv0", "-http-listen", "0.0.0.0:4022", "-allowed-group", "239.0.0.0/8", "-allowed-client", "192.168.4.0/24", "-allowed-port", "7980"},
		{"-multicast-input", "iptv0", "-http-listen", "192.168.4.1:4022", "-allowed-group", "10.0.0.0/8", "-allowed-client", "192.168.4.0/24", "-allowed-port", "7980"},
		{"-multicast-input", "iptv0", "-http-listen", "192.168.4.1:4022", "-allowed-group", "239.0.0.0/8", "-allowed-client", "192.168.4.0/24", "-allowed-port", "9000-8000"},
		{"-multicast-input", "iptv0", "-http-listen", "192.168.4.1:4022", "-allowed-group", "239.0.0.0/8", "-allowed-client", "192.168.4.0/24", "-allowed-port", "7980", "-max-clients", "17"},
	}
	for _, args := range tests {
		if _, err := parseTestConfig(t, args...); err == nil {
			t.Fatalf("expected error for %v", args)
		}
	}
}
