package prudp

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"testing"
	"time"
)

func TestReliableDataPacketsFragmentAndRoundTrip(t *testing.T) {
	server := NewServer(30671, "access", "secret", slog.Default())
	connection := NewConnection(&net.UDPAddr{IP: net.ParseIP("192.0.2.20"), Port: 9103}, 30671)
	body := make([]byte, 1810)
	for index := range body {
		body[index] = byte(index)
	}

	packets, err := server.reliableDataPackets(connection, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 3 {
		t.Fatalf("got %d fragments, want 3", len(packets))
	}
	var reassembled []byte
	for index, packet := range packets {
		wantFragmentID := uint8(len(packets) - index - 1)
		if packet.FragmentID != wantFragmentID {
			t.Fatalf("fragment %d ID=%d, want %d", index, packet.FragmentID, wantFragmentID)
		}
		if packet.PacketID != uint16(index+1) {
			t.Fatalf("fragment %d packet ID=%d, want %d", index, packet.PacketID, index+1)
		}
		decoded, err := server.payload.Decode(packet.Payload)
		if err != nil {
			t.Fatal(err)
		}
		reassembled = append(reassembled, decoded...)
		if rawSize := len(server.packet.Encode(packet)); rawSize > 1000 {
			t.Fatalf("fragment %d encoded to %d bytes, want at most 1000", index, rawSize)
		}
	}
	if !bytes.Equal(reassembled, body) {
		t.Fatalf("reassembled %d bytes, want %d", len(reassembled), len(body))
	}
}

func TestReliableDataPacketsLeaveSmallBodyUnfragmented(t *testing.T) {
	server := NewServer(30671, "access", "secret", slog.Default())
	connection := NewConnection(&net.UDPAddr{IP: net.ParseIP("192.0.2.20"), Port: 9103}, 30671)
	packets, err := server.reliableDataPackets(connection, []byte("small"))
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 1 || packets[0].FragmentID != 0 {
		t.Fatalf("unexpected packets: %#v", packets)
	}
}

func TestLiveSYNHandshake(t *testing.T) {
	server := NewServer(0, "access", "", slog.Default())
	if err := server.Listen("127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()

	client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	codec := Codec{AccessKey: []byte("access")}
	syn := Packet{
		Type: TypeSYN, Flags: FlagNeedACK,
		SourceType: ClientType, SourcePort: ClientPort,
		DestinationType: ServerType, DestinationPort: ServerPort,
	}
	serverAddress := server.socket.LocalAddr().(*net.UDPAddr)
	if _, err := client.WriteToUDP(codec.Encode(syn), serverAddress); err != nil {
		t.Fatal(err)
	}
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 2048)
	size, _, err := client.ReadFromUDP(buffer)
	if err != nil {
		t.Fatal(err)
	}
	packets, err := codec.Decode(buffer[:size])
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 1 || packets[0].Type != TypeSYN || packets[0].Flags&FlagACK == 0 {
		t.Fatalf("unexpected SYN response %#v", packets)
	}
	if packets[0].ConnectionSignature == [4]byte{} {
		t.Fatal("server returned an empty connection signature")
	}

	cancel()
	_ = server.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("server read loop did not stop")
	}
}
