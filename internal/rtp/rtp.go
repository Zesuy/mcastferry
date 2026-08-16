// Package rtp implements conservative RTP/MPEG-TS detection and decapsulation.
package rtp

import (
	"encoding/binary"
	"errors"
)

const tsPacketSize = 188

type Mode int

const (
	ModeAuto Mode = iota
	ModeRaw
	ModeRawTS
	ModeRTPTS
)

func (m Mode) String() string {
	switch m {
	case ModeAuto:
		return "auto"
	case ModeRaw:
		return "raw"
	case ModeRawTS:
		return "raw-ts"
	case ModeRTPTS:
		return "rtp-ts"
	default:
		return "unknown"
	}
}

type PacketResult struct {
	Payload []byte
	Mode    Mode
	Valid   bool
}

type Header struct {
	Marker       bool
	PayloadType  uint8
	Sequence     uint16
	Timestamp    uint32
	SSRC         uint32
	CSRCCount    uint8
	HasExtension bool
	Padding      int
	HeaderLength int
	Payload      []byte
}

var ErrMalformed = errors.New("malformed RTP packet")

func Parse(packet []byte) (Header, error) {
	if len(packet) < 12 || packet[0]>>6 != 2 {
		return Header{}, ErrMalformed
	}
	padding := packet[0]&0x20 != 0
	extension := packet[0]&0x10 != 0
	cc := int(packet[0] & 0x0f)
	headerLen := 12 + cc*4
	if headerLen > len(packet) {
		return Header{}, ErrMalformed
	}
	if extension {
		if headerLen+4 > len(packet) {
			return Header{}, ErrMalformed
		}
		words := int(binary.BigEndian.Uint16(packet[headerLen+2 : headerLen+4]))
		if words > (len(packet)-headerLen-4)/4 {
			return Header{}, ErrMalformed
		}
		headerLen += 4 + words*4
	}
	payloadEnd := len(packet)
	paddingLen := 0
	if padding {
		if payloadEnd <= headerLen {
			return Header{}, ErrMalformed
		}
		paddingLen = int(packet[payloadEnd-1])
		if paddingLen == 0 || paddingLen > payloadEnd-headerLen {
			return Header{}, ErrMalformed
		}
		payloadEnd -= paddingLen
	}
	if payloadEnd <= headerLen {
		return Header{}, ErrMalformed
	}
	return Header{
		Marker: packet[1]&0x80 != 0, PayloadType: packet[1] & 0x7f,
		Sequence:  binary.BigEndian.Uint16(packet[2:4]),
		Timestamp: binary.BigEndian.Uint32(packet[4:8]),
		SSRC:      binary.BigEndian.Uint32(packet[8:12]),
		CSRCCount: uint8(cc), HasExtension: extension,
		Padding: paddingLen, HeaderLength: headerLen,
		Payload: packet[headerLen:payloadEnd],
	}, nil
}

func IsMPEGTS(payload []byte) bool {
	// Requiring two sync intervals avoids locking an arbitrary RAW datagram merely
	// because its first byte happens to be 0x47. A single TS packet remains safe:
	// it falls back to byte-for-byte RAW forwarding.
	if len(payload) < 2*tsPacketSize || len(payload)%tsPacketSize != 0 {
		return false
	}
	for offset := 0; offset < len(payload); offset += tsPacketSize {
		if payload[offset] != 0x47 {
			return false
		}
	}
	return true
}

type Stats struct {
	InvalidRTP  uint64
	SequenceGap uint64
	SSRCChange  uint64
}

type Processor struct {
	mode       Mode
	haveRTP    bool
	lastSeq    uint16
	lastSSRC   uint32
	statistics Stats
}

func NewProcessor() *Processor { return &Processor{mode: ModeAuto} }

func (p *Processor) Mode() Mode { return p.mode }

func (p *Processor) Stats() Stats { return p.statistics }

func (p *Processor) Process(datagram []byte) PacketResult {
	if p.mode == ModeAuto {
		return p.detect(datagram)
	}
	switch p.mode {
	case ModeRaw, ModeRawTS:
		return PacketResult{Payload: datagram, Mode: p.mode, Valid: true}
	case ModeRTPTS:
		return p.processRTP(datagram)
	default:
		return PacketResult{Mode: p.mode}
	}
}

func (p *Processor) detect(datagram []byte) PacketResult {
	if len(datagram) == 0 {
		return PacketResult{Payload: datagram, Mode: ModeAuto, Valid: true}
	}
	if IsMPEGTS(datagram) {
		p.mode = ModeRawTS
		return PacketResult{Payload: datagram, Mode: p.mode, Valid: true}
	}
	if header, err := Parse(datagram); err == nil && IsMPEGTS(header.Payload) {
		p.mode = ModeRTPTS
		p.observe(header)
		return PacketResult{Payload: header.Payload, Mode: p.mode, Valid: true}
	}
	p.mode = ModeRaw
	return PacketResult{Payload: datagram, Mode: p.mode, Valid: true}
}

func (p *Processor) processRTP(datagram []byte) PacketResult {
	header, err := Parse(datagram)
	if err != nil || !IsMPEGTS(header.Payload) {
		p.statistics.InvalidRTP++
		return PacketResult{Mode: p.mode, Valid: false}
	}
	p.observe(header)
	return PacketResult{Payload: header.Payload, Mode: p.mode, Valid: true}
}

func (p *Processor) observe(header Header) {
	if p.haveRTP {
		if header.Sequence != p.lastSeq+1 {
			p.statistics.SequenceGap++
		}
		if header.SSRC != p.lastSSRC {
			p.statistics.SSRCChange++
		}
	}
	p.haveRTP = true
	p.lastSeq = header.Sequence
	p.lastSSRC = header.SSRC
}
