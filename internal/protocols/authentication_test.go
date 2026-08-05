package protocols

import (
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"stranglehold-go-server/internal/config"
	"stranglehold-go-server/internal/prudp"
	"stranglehold-go-server/internal/rmc"
	"stranglehold-go-server/internal/state"
	"stranglehold-go-server/internal/store"
	"stranglehold-go-server/internal/wire"
)

func newAuthenticationTestServices(t *testing.T) *Services {
	t.Helper()
	database, err := store.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{PublicHost: "192.0.2.10", SecurePort: 30671, NATPort: 30672}
	return NewServices(
		cfg, database, state.NewIdentityRegistry(),
		state.NewGatheringRegistry(false, logger), logger,
	)
}

func TestGuestLoginCredentialAndConnectProof(t *testing.T) {
	services := newAuthenticationTestServices(t)
	connection := prudp.NewConnection(
		&net.UDPAddr{IP: net.ParseIP("192.0.2.20"), Port: 9103}, 30670,
	)
	var params wire.Writer
	params.QString("guest")
	message := services.login(nil, connection, rmc.Request{
		Protocol: authenticationProtocol, Method: loginMethod,
		Call: 4, Params: params.Data(),
	})
	response, ok := message.(rmc.ResponseOK)
	if !ok {
		t.Fatalf("login returned %T", message)
	}
	reader := wire.NewReader(response.Return)
	result, err := reader.U32()
	if err != nil || result != 0 {
		t.Fatalf("result=%d err=%v", result, err)
	}
	pid, err := reader.U32()
	if err != nil || pid != 100 {
		t.Fatalf("pid=%d err=%v", pid, err)
	}
	envelope, err := reader.QBuffer()
	if err != nil {
		t.Fatal(err)
	}
	if len(envelope) != 36 {
		t.Fatalf("credential envelope length=%d, want 36", len(envelope))
	}
	plain, err := decryptAndVerify(deriveLoginKey([]byte("h7fyctiuucf"), 100, false), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(plain[:4]); got != 100 {
		t.Fatalf("credential pid=%d", got)
	}
	sessionKey := plain[4:20]
	stationURL, err := reader.QString()
	if err != nil || !strings.Contains(stationURL, "port=30671") {
		t.Fatalf("station URL=%q err=%v", stationURL, err)
	}

	connectPlain := binary.LittleEndian.AppendUint32(nil, 0)
	connectPlain = binary.LittleEndian.AppendUint32(connectPlain, 0)
	connectPlain = binary.LittleEndian.AppendUint32(connectPlain, 123)
	connectEnvelope, err := encryptThenMAC(sessionKey, connectPlain)
	if err != nil {
		t.Fatal(err)
	}
	incoming := []byte{0}
	incoming = binary.LittleEndian.AppendUint32(incoming, 0)
	incoming = binary.LittleEndian.AppendUint32(incoming, uint32(len(connectEnvelope)))
	incoming = append(incoming, connectEnvelope...)

	secureConnection := prudp.NewConnection(
		&net.UDPAddr{IP: net.ParseIP("192.0.2.20"), Port: 9103}, 30671,
	)
	ack, err := services.HandleConnectACK(secureConnection, incoming)
	if err != nil {
		t.Fatal(err)
	}
	if len(ack) != 8 || binary.LittleEndian.Uint32(ack[:4]) != 4 ||
		binary.LittleEndian.Uint32(ack[4:]) != 124 {
		t.Fatalf("CONNECT ACK=%x", ack)
	}
	if secureConnection.UserPID != 100 {
		t.Fatalf("secure connection pid=%d", secureConnection.UserPID)
	}
}

func TestLoginIdentitiesAreScopedToRemoteEndpoint(t *testing.T) {
	services := newAuthenticationTestServices(t)
	ip := net.ParseIP("192.0.2.20")

	login := func(username string, port int) *prudp.Connection {
		connection := prudp.NewConnection(&net.UDPAddr{IP: ip, Port: port}, 30670)
		var params wire.Writer
		params.QString(username)
		if _, ok := services.login(nil, connection, rmc.Request{
			Protocol: authenticationProtocol, Method: loginMethod, Call: 4, Params: params.Data(),
		}).(rmc.ResponseOK); !ok {
			t.Fatalf("login for %s failed", username)
		}
		return connection
	}

	first := login("player_one", 9103)
	second := login("player_two", 51386)
	if first.UserPID == second.UserPID {
		t.Fatalf("logins received the same PID %d", first.UserPID)
	}

	firstSecure := prudp.NewConnection(&net.UDPAddr{IP: ip, Port: 9103}, 30671)
	secondSecure := prudp.NewConnection(&net.UDPAddr{IP: ip, Port: 51386}, 30671)
	services.attachIdentity(firstSecure)
	services.attachIdentity(secondSecure)
	if firstSecure.UserPID != first.UserPID {
		t.Fatalf("first secure PID=%d, want %d", firstSecure.UserPID, first.UserPID)
	}
	if secondSecure.UserPID != second.UserPID {
		t.Fatalf("second secure PID=%d, want %d", secondSecure.UserPID, second.UserPID)
	}
}
