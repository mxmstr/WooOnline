package state

import (
	"encoding/binary"
	"log/slog"
	"testing"
)

func TestStrangleholdGatheringRendering(t *testing.T) {
	display := append([]byte("host 1,2,3"), 0)
	raw := make([]byte, displayQStringOffset+2+len(display)+strangleholdDerivedSize)
	binary.LittleEndian.PutUint16(raw[:2], 10)
	copy(raw[2:12], "SparkGame\x00")
	binary.LittleEndian.PutUint16(raw[displayQStringOffset:displayQStringOffset+2], uint16(len(display)))
	copy(raw[displayQStringOffset+2:], display)

	var urls []byte
	urls = binary.LittleEndian.AppendUint32(urls, 1)
	url := append([]byte("prudp:/address=127.0.0.1;port=1234"), 0)
	urls = binary.LittleEndian.AppendUint16(urls, uint16(len(url)))
	urls = append(urls, url...)
	raw = append(raw, 0) // observed CreateGathering option byte
	raw = append(raw, urls...)

	registry := NewGatheringRegistry(false, slog.Default())
	gathering := registry.Create(123, "127.0.0.1:5000", raw, nil)
	if !gathering.IsStrangleholdLayout() {
		t.Fatal("fixture was not recognized as Stranglehold")
	}
	if got := gathering.HostDisplayName(); got != "host" {
		t.Fatalf("host display name %q", got)
	}
	rendered := gathering.RenderSettingsForBrowse()
	if got := binary.LittleEndian.Uint32(rendered[0x14:0x18]); got != gathering.ID {
		t.Fatalf("gathering id %d", got)
	}
	if got := binary.LittleEndian.Uint32(rendered[0x18:0x1c]); got != 123 {
		t.Fatalf("owner pid %d", got)
	}
	group := gathering.RenderURLGroupForBrowse()
	if got := binary.LittleEndian.Uint32(group[:4]); got != gathering.ID {
		t.Fatalf("url group id %d", got)
	}
	if got := binary.LittleEndian.Uint32(group[4:8]); got != 1 {
		t.Fatalf("url count %d", got)
	}
}
