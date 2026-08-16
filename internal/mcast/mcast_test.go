package mcast

import (
	"errors"
	"net/netip"
	"testing"
)

func testConfig() Config {
	return Config{Group: netip.MustParseAddr("239.1.2.3"), Port: 7980, IfIndex: 7, ReceiveBuffer: 4 << 20}
}

func TestConfigValidate(t *testing.T) {
	if err := testConfig().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []Config{
		{},
		{Group: netip.MustParseAddr("10.0.0.1"), Port: 7980, IfIndex: 7, ReceiveBuffer: 1024},
		{Group: netip.MustParseAddr("239.1.2.3"), IfIndex: 7, ReceiveBuffer: 1024},
		{Group: netip.MustParseAddr("239.1.2.3"), Port: 7980, ReceiveBuffer: 1024},
	} {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected invalid config: %+v", cfg)
		}
	}
}

func TestValidateDatagram(t *testing.T) {
	cfg := testConfig()
	source := netip.MustParseAddrPort("192.0.2.1:5000")
	d, err := validateDatagram(cfg, nil, cfg.Group, cfg.IfIndex, source, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Payload) != 0 || d.Source != source || d.Group != cfg.Group || d.IfIndex != cfg.IfIndex {
		t.Fatalf("unexpected zero datagram: %+v", d)
	}
	for _, tc := range []struct {
		group     netip.Addr
		ifIndex   int
		truncated bool
	}{
		{netip.MustParseAddr("239.1.2.4"), cfg.IfIndex, false},
		{cfg.Group, cfg.IfIndex + 1, false},
		{cfg.Group, cfg.IfIndex, true},
	} {
		_, err := validateDatagram(cfg, []byte("x"), tc.group, tc.ifIndex, source, tc.truncated)
		if !errors.Is(err, ErrDatagramRejected) {
			t.Fatalf("expected rejected datagram for %+v, got %v", tc, err)
		}
	}
}
