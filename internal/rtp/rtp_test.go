package rtp

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func tsPayload(packets int) []byte {
	payload := make([]byte, packets*tsPacketSize)
	for i := 0; i < packets; i++ {
		payload[i*tsPacketSize] = 0x47
		payload[i*tsPacketSize+1] = byte(i)
	}
	return payload
}

func rtpPacket(payload []byte, sequence uint16, ssrc uint32, csrcs int, extension []byte, padding int) []byte {
	first := byte(0x80 | csrcs)
	if extension != nil {
		first |= 0x10
	}
	if padding > 0 {
		first |= 0x20
	}
	headerLen := 12 + csrcs*4
	if extension != nil {
		headerLen += 4 + len(extension)
	}
	packet := make([]byte, headerLen+len(payload)+padding)
	packet[0], packet[1] = first, 33
	binary.BigEndian.PutUint16(packet[2:4], sequence)
	binary.BigEndian.PutUint32(packet[4:8], 1234)
	binary.BigEndian.PutUint32(packet[8:12], ssrc)
	for i := 0; i < csrcs; i++ {
		binary.BigEndian.PutUint32(packet[12+i*4:16+i*4], uint32(i+1))
	}
	offset := 12 + csrcs*4
	if extension != nil {
		binary.BigEndian.PutUint16(packet[offset:offset+2], 0x1000)
		binary.BigEndian.PutUint16(packet[offset+2:offset+4], uint16(len(extension)/4))
		copy(packet[offset+4:], extension)
		offset += 4 + len(extension)
	}
	copy(packet[offset:], payload)
	if padding > 0 {
		packet[len(packet)-1] = byte(padding)
	}
	return packet
}

func TestParseVariableHeader(t *testing.T) {
	payload := tsPayload(7)
	packet := rtpPacket(payload, 42, 99, 2, []byte{1, 2, 3, 4, 5, 6, 7, 8}, 6)
	header, err := Parse(packet)
	if err != nil {
		t.Fatal(err)
	}
	if header.Sequence != 42 || header.SSRC != 99 || header.CSRCCount != 2 || !header.HasExtension || header.Padding != 6 {
		t.Fatalf("unexpected header: %+v", header)
	}
	if !bytes.Equal(header.Payload, payload) {
		t.Fatal("payload boundary mismatch")
	}
}

func TestProcessorModes(t *testing.T) {
	rawTS := tsPayload(2)
	p := NewProcessor()
	if result := p.Process(rawTS); !result.Valid || result.Mode != ModeRawTS || !bytes.Equal(result.Payload, rawTS) {
		t.Fatalf("raw TS result: %+v", result)
	}

	p = NewProcessor()
	opaque := []byte{0x47, 1, 2, 3}
	if result := p.Process(opaque); !result.Valid || result.Mode != ModeRaw || !bytes.Equal(result.Payload, opaque) {
		t.Fatalf("RAW result: %+v", result)
	}

	p = NewProcessor()
	packet := rtpPacket(tsPayload(7), 7, 8, 0, nil, 0)
	if result := p.Process(packet); !result.Valid || result.Mode != ModeRTPTS || !IsMPEGTS(result.Payload) {
		t.Fatalf("RTP result: %+v", result)
	}
}

func TestFirstEmptyDatagramDoesNotLockMode(t *testing.T) {
	p := NewProcessor()
	result := p.Process(nil)
	if !result.Valid || result.Mode != ModeAuto || p.Mode() != ModeAuto {
		t.Fatalf("empty result: %+v, mode=%s", result, p.Mode())
	}
	opaque := []byte("opaque")
	result = p.Process(opaque)
	if result.Mode != ModeRaw || !bytes.Equal(result.Payload, opaque) {
		t.Fatalf("first non-empty datagram lost: %+v", result)
	}
}

func TestLockedRTPDropsMalformedAndCountsEvents(t *testing.T) {
	p := NewProcessor()
	p.Process(rtpPacket(tsPayload(2), 10, 1, 0, nil, 0))
	if result := p.Process([]byte{0x80}); result.Valid || len(result.Payload) != 0 {
		t.Fatalf("malformed packet leaked: %+v", result)
	}
	result := p.Process(rtpPacket(tsPayload(2), 12, 2, 0, nil, 0))
	if !result.Valid {
		t.Fatal("valid packet after malformed packet was rejected")
	}
	stats := p.Stats()
	if stats.InvalidRTP != 1 || stats.SequenceGap != 1 || stats.SSRCChange != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestParseRejectsTruncatedExtensionAndPadding(t *testing.T) {
	for _, packet := range [][]byte{
		{0x80},
		append([]byte{0x90, 33, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1}, 0, 0, 0, 2),
		append([]byte{0xa0, 33, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1}, 0),
	} {
		if _, err := Parse(packet); err == nil {
			t.Fatalf("expected malformed packet %x", packet)
		}
	}
}
