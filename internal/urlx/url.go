// Package urlx parses the deliberately small udpxy-compatible URL surface.
package urlx

import (
	"errors"
	"net/netip"
	"strconv"
	"strings"
)

type Target struct {
	Group netip.Addr
	Port  uint16
}

func Parse(path string) (Target, error) {
	if !strings.HasPrefix(path, "/udp/") || !strings.HasSuffix(path, "/") {
		return Target{}, errors.New("path must be /udp/<group>:<port>/")
	}
	if strings.ContainsAny(path, "?#%\\\t\r\n ") {
		return Target{}, errors.New("path contains unsupported characters")
	}
	address := strings.TrimSuffix(strings.TrimPrefix(path, "/udp/"), "/")
	if address == "" || strings.Contains(address, "/") || strings.Count(address, ":") != 1 {
		return Target{}, errors.New("path contains an invalid stream target")
	}
	groupText, portText, _ := strings.Cut(address, ":")
	group, err := netip.ParseAddr(groupText)
	if err != nil || !group.Is4() || !group.IsMulticast() {
		return Target{}, errors.New("group must be an IPv4 multicast address")
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return Target{}, errors.New("port must be a decimal UDP port")
	}
	return Target{Group: group, Port: uint16(port)}, nil
}
