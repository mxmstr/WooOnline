package prudp

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestPacketRoundTrip(t *testing.T) {
	codec := Codec{AccessKey: []byte("access")}
	packet := Packet{
		Type: TypeData, Flags: FlagReliable | FlagNeedACK | FlagHasSize,
		SourceType: 3, SourcePort: 1, DestinationType: 3, DestinationPort: 15,
		SessionID: 9, PacketID: 42, FragmentID: 2, Payload: []byte{1, 2, 3},
	}
	packet.Signature = [4]byte{4, 5, 6, 7}
	raw := codec.Encode(packet)
	const pythonReference = "313f7209040506072a00020300010203a8"
	if got := hex.EncodeToString(raw); got != pythonReference {
		t.Fatalf("encoded %s, Python PRUDPMessageV0 produced %s", got, pythonReference)
	}
	decoded, err := codec.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 1 || decoded[0].PacketID != packet.PacketID || !bytes.Equal(decoded[0].Payload, packet.Payload) {
		t.Fatalf("decoded %#v", decoded)
	}
}

func TestLZOInitialLiteral(t *testing.T) {
	encoded := append([]byte{17 + 5}, []byte("hello")...)
	got, err := lzo1xDecompress(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestPayloadRoundTrip(t *testing.T) {
	codec := NewPayloadCodec("secret")
	want := []byte("hello RMC")
	encoded, err := codec.Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := codec.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
