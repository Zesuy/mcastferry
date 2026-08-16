//go:build linux

package mcast

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sync"
	"syscall"
	"time"
)

const ipMulticastAll = 49

type socketReceiver struct {
	cfg        Config
	conn       *net.UDPConn
	membership syscall.IPMreqn
	closeOnce  sync.Once
	closeErr   error
}

func Open(cfg Config) (Receiver, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, syscall.IPPROTO_UDP)
	if err != nil {
		return nil, fmt.Errorf("create multicast socket: %w", err)
	}
	owned := true
	defer func() {
		if owned {
			_ = syscall.Close(fd)
		}
	}()
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return nil, fmt.Errorf("set SO_REUSEADDR: %w", err)
	}
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, cfg.ReceiveBuffer); err != nil {
		return nil, fmt.Errorf("set SO_RCVBUF: %w", err)
	}
	if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, ipMulticastAll, 0); err != nil {
		return nil, fmt.Errorf("set IP_MULTICAST_ALL=0: %w", err)
	}
	if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_PKTINFO, 1); err != nil {
		return nil, fmt.Errorf("enable IP_PKTINFO: %w", err)
	}
	if err := syscall.Bind(fd, &syscall.SockaddrInet4{Port: int(cfg.Port)}); err != nil {
		return nil, fmt.Errorf("bind multicast port %d: %w", cfg.Port, err)
	}
	membership := syscall.IPMreqn{Ifindex: int32(cfg.IfIndex)}
	group := cfg.Group.As4()
	copy(membership.Multiaddr[:], group[:])
	if err := syscall.SetsockoptIPMreqn(fd, syscall.IPPROTO_IP, syscall.IP_ADD_MEMBERSHIP, &membership); err != nil {
		return nil, fmt.Errorf("join %s on ifindex %d: %w", cfg.Group, cfg.IfIndex, err)
	}
	file := os.NewFile(uintptr(fd), "mcastferry-multicast")
	packetConn, err := net.FilePacketConn(file)
	fileCloseErr := file.Close()
	owned = false
	if err != nil {
		return nil, fmt.Errorf("adopt multicast socket: %w", err)
	}
	if fileCloseErr != nil {
		_ = packetConn.Close()
		return nil, fmt.Errorf("release original multicast descriptor: %w", fileCloseErr)
	}
	udp, ok := packetConn.(*net.UDPConn)
	if !ok {
		_ = packetConn.Close()
		return nil, errors.New("multicast socket did not produce a UDP connection")
	}
	return &socketReceiver{cfg: cfg, conn: udp, membership: membership}, nil
}

func (r *socketReceiver) Read(ctx context.Context) (Datagram, error) {
	payload := make([]byte, 65535)
	oob := make([]byte, syscall.CmsgSpace(12))
	if deadline, ok := ctx.Deadline(); ok {
		_ = r.conn.SetReadDeadline(deadline)
	} else {
		_ = r.conn.SetReadDeadline(time.Time{})
	}
	stop := context.AfterFunc(ctx, func() { _ = r.conn.SetReadDeadline(time.Now()) })
	n, oobn, flags, source, err := r.conn.ReadMsgUDP(payload, oob)
	stop()
	if err != nil {
		if ctx.Err() != nil {
			return Datagram{}, ctx.Err()
		}
		if errors.Is(err, net.ErrClosed) {
			return Datagram{}, ErrClosed
		}
		return Datagram{}, err
	}
	group, ifIndex, err := parsePacketInfo(oob[:oobn])
	if err != nil {
		return Datagram{}, fmt.Errorf("%w: %v", ErrDatagramRejected, err)
	}
	sourceAddr := source.AddrPort()
	return validateDatagram(r.cfg, payload[:n], group, ifIndex, sourceAddr, flags&syscall.MSG_TRUNC != 0)
}

func (r *socketReceiver) Close() error {
	r.closeOnce.Do(func() {
		var leaveErr error
		raw, err := r.conn.SyscallConn()
		if err == nil {
			controlErr := raw.Control(func(fd uintptr) {
				leaveErr = syscall.SetsockoptIPMreqn(int(fd), syscall.IPPROTO_IP, syscall.IP_DROP_MEMBERSHIP, &r.membership)
			})
			if controlErr != nil {
				leaveErr = controlErr
			}
		}
		r.closeErr = errors.Join(leaveErr, r.conn.Close())
	})
	return r.closeErr
}

func parsePacketInfo(oob []byte) (netip.Addr, int, error) {
	messages, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		return netip.Addr{}, 0, err
	}
	for _, message := range messages {
		if message.Header.Level != syscall.IPPROTO_IP || message.Header.Type != syscall.IP_PKTINFO || len(message.Data) < 12 {
			continue
		}
		ifIndex := int(int32(binary.NativeEndian.Uint32(message.Data[:4])))
		group := netip.AddrFrom4([4]byte{message.Data[8], message.Data[9], message.Data[10], message.Data[11]})
		return group, ifIndex, nil
	}
	return netip.Addr{}, 0, errors.New("IP_PKTINFO control message missing")
}
