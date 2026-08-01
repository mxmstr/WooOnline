package prudp

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"
)

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
