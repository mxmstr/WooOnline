package protocols

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestStatsProcessedEventUsesQStringLength(t *testing.T) {
	stats := []byte{0xaa, 0xbb, 0xcc}
	raw := buildStatsProcessedEvent(10000, 123, 7, stats)
	if got := binary.LittleEndian.Uint16(raw[16:18]); got != uint16(len(stats)) {
		t.Fatalf("stats buffer length=%d, want %d", got, len(stats))
	}
	if got := raw[18:]; !bytes.Equal(got, stats) {
		t.Fatalf("stats buffer=%x, want %x", got, stats)
	}
}
