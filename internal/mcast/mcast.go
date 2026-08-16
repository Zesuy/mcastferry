// Package mcast provides a constrained IPv4 ASM multicast receiver.
package mcast

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
)

var (
	ErrDatagramRejected = errors.New("multicast datagram rejected")
	ErrClosed           = errors.New("multicast receiver closed")
)

type Config struct {
	Group         netip.Addr
	Port          uint16
	IfIndex       int
	ReceiveBuffer int
}

func (c Config) Validate() error {
	if !c.Group.IsValid() || !c.Group.Is4() || !c.Group.IsMulticast() {
		return errors.New("group must be an IPv4 multicast address")
	}
	if c.Port == 0 {
		return errors.New("port must be non-zero")
	}
	if c.IfIndex <= 0 {
		return errors.New("interface index must be positive")
	}
	if c.ReceiveBuffer <= 0 || c.ReceiveBuffer > 16<<20 {
		return errors.New("receive buffer must be between 1 and 16777216 bytes")
	}
	return nil
}

type Datagram struct {
	Payload   []byte
	Group     netip.Addr
	IfIndex   int
	Source    netip.AddrPort
	Truncated bool
}

type Receiver interface {
	Read(context.Context) (Datagram, error)
	Close() error
}

func validateDatagram(cfg Config, payload []byte, group netip.Addr, ifIndex int, source netip.AddrPort, truncated bool) (Datagram, error) {
	if group != cfg.Group || ifIndex != cfg.IfIndex {
		return Datagram{}, fmt.Errorf("%w: destination group or interface mismatch", ErrDatagramRejected)
	}
	if truncated {
		return Datagram{}, fmt.Errorf("%w: payload truncated", ErrDatagramRejected)
	}
	return Datagram{Payload: payload, Group: group, IfIndex: ifIndex, Source: source}, nil
}
