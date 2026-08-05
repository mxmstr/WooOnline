package protocols

import (
	"context"
	"encoding/hex"
	"log/slog"
	"unicode/utf16"

	"stranglehold-go-server/internal/prudp"
	"stranglehold-go-server/internal/rmc"
	"stranglehold-go-server/internal/wire"
)

const (
	accountProtocol      uint8  = 25
	getAccountInfoMethod uint32 = 9
	lookupAccountMethod  uint32 = 21
)

var emptyAccountInfo = mustDecodeHex(
	"01" +
		"1600" + "4163636f756e74496e666f5075626c69634461746100" +
		"26000000" + "22000000" + "20000000" +
		"000000000000000000000000000000000000000000000000000000000000",
)

func (s *Services) registerAccounts(dispatcher *rmc.Dispatcher) {
	dispatcher.Register(accountProtocol, getAccountInfoMethod, s.getAccountInfo)
	dispatcher.Register(accountProtocol, lookupAccountMethod, s.lookupOrCreateAccount)
}

func (s *Services) getAccountInfo(_ *rmc.Dispatcher, _ *prudp.Connection, request rmc.Request) rmc.Message {
	if len(request.Params) < 4 {
		return rmc.ResponseError{Protocol: accountProtocol, Call: request.Call, Code: 0x80030001}
	}
	return rmc.ResponseOK{Protocol: accountProtocol, Method: getAccountInfoMethod, Call: request.Call, Return: emptyAccountInfo}
}

func (s *Services) lookupOrCreateAccount(_ *rmc.Dispatcher, connection *prudp.Connection, request rmc.Request) rmc.Message {
	reader := wire.NewReader(request.Params)
	playerName, err := reader.QString()
	if err != nil {
		return rmc.ResponseError{Protocol: accountProtocol, Call: request.Call, Code: 0x80030001}
	}
	stringKey, err := reader.QString()
	if err != nil {
		return rmc.ResponseError{Protocol: accountProtocol, Call: request.Call, Code: 0x80030001}
	}
	account, err := s.Store.FindOrCreateAccount(context.Background(), playerName)
	if err != nil {
		s.Logger.Error("lookup account", "name", playerName, "error", err)
		return rmc.ResponseError{Protocol: accountProtocol, Call: request.Call, Code: 0x80020001}
	}
	connection.UserPID = account.PID
	s.Identity.SetAuthenticatedPID(connection.RemoteKey(), account.PID)
	password := []byte("h7fyctiuucf")
	preHashed := false
	if stringKey != "" {
		if decoded, decodeErr := hex.DecodeString(stringKey); decodeErr == nil {
			password = decoded
			preHashed = true
		} else {
			password = []byte(stringKey)
		}
	}
	if err := s.Store.SetAccountCredential(context.Background(), account.PID, password, preHashed); err != nil {
		s.Logger.Error("persist account credential", "name", playerName, "error", err)
		return rmc.ResponseError{Protocol: accountProtocol, Call: request.Call, Code: 0x80020001}
	}
	s.authMu.Lock()
	s.credentials[playerName] = credential{
		PID: account.PID, Password: append([]byte(nil), password...),
		PreHashed: preHashed,
	}
	s.authMu.Unlock()

	encodedName := utf16.Encode([]rune(playerName + "\x00"))
	var payload wire.Writer
	payload.U32(account.PID)
	payload.U16(uint16(len(encodedName)))
	for _, codeUnit := range encodedName {
		payload.U16(codeUnit)
	}
	var out wire.Writer
	out.U32(0)
	out.U8(1)
	out.QString("BasicAccountInfo")
	out.U32(uint32(payload.Len() + 4))
	out.U32(uint32(payload.Len()))
	out.Bytes(payload.Data())

	s.Logger.Info("account ready", "remote", connection.Remote, "name", playerName, "pid", account.PID, "key_supplied", stringKey != "")
	return rmc.ResponseOK{Protocol: accountProtocol, Method: lookupAccountMethod, Call: request.Call, Return: out.Data()}
}

func mustDecodeHex(value string) []byte {
	data, err := hex.DecodeString(value)
	if err != nil {
		slog.Error("invalid embedded hex", "error", err)
		panic(err)
	}
	return data
}
