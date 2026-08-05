package protocols

import (
	"net"
	"testing"

	"stranglehold-go-server/internal/prudp"
	"stranglehold-go-server/internal/rmc"
)

func TestStartGameAcknowledgesRestart(t *testing.T) {
	services := &Services{}
	connection := prudp.NewConnection(
		&net.UDPAddr{IP: net.ParseIP("192.0.2.20"), Port: 9103}, 30671,
	)
	request := rmc.Request{
		Protocol: gameProtocol,
		Method:   startGameMethod,
		Call:     42,
	}

	message := services.lifecycle(nil, connection, request)
	response, ok := message.(rmc.ResponseOK)
	if !ok {
		t.Fatalf("start game returned %T", message)
	}
	if response.Protocol != gameProtocol || response.Method != startGameMethod || response.Call != request.Call {
		t.Fatalf("unexpected response: %+v", response)
	}
	if len(response.Return) != 0 {
		t.Fatalf("start game returned %d bytes, want none", len(response.Return))
	}
}

func TestReportStatsReturnsVoidResponse(t *testing.T) {
	services := newAuthenticationTestServices(t)
	connection := prudp.NewConnection(
		&net.UDPAddr{IP: net.ParseIP("192.0.2.20"), Port: 9103}, 30671,
	)
	connection.UserPID = 10000
	request := rmc.Request{
		Protocol: gameProtocol,
		Method:   reportStatsMethod,
		Call:     43,
		// An invalid report is sufficient here: response cardinality is part of
		// the RPC contract and must not depend on report contents.
		Params: []byte{0},
	}

	message := services.lifecycle(nil, connection, request)
	response, ok := message.(rmc.ResponseOK)
	if !ok {
		t.Fatalf("report stats returned %T", message)
	}
	if len(response.Return) != 0 {
		t.Fatalf("report stats returned %d bytes, want none", len(response.Return))
	}
}
