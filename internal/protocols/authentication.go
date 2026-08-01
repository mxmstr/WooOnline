package protocols

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rc4"
	"encoding/binary"
	"fmt"

	"stranglehold-go-server/internal/prudp"
	"stranglehold-go-server/internal/rmc"
	"stranglehold-go-server/internal/wire"
)

const (
	authenticationProtocol uint8  = 10
	loginMethod            uint32 = 1
)

type credential struct {
	PID       uint32
	Password  []byte
	PreHashed bool
}

var loginTrailer = []byte{
	0, 0, 0, 0, 1, 0, 0, 1, 0, 0,
}

func (s *Services) registerAuthentication(dispatcher *rmc.Dispatcher) {
	dispatcher.Register(authenticationProtocol, loginMethod, s.login)
}

func (s *Services) login(_ *rmc.Dispatcher, connection *prudp.Connection, request rmc.Request) rmc.Message {
	reader := wire.NewReader(request.Params)
	username, err := reader.QString()
	if err != nil {
		return rmc.ResponseError{Protocol: authenticationProtocol, Call: request.Call, Code: 0x80030001}
	}

	s.authMu.RLock()
	credentials, found := s.credentials[username]
	s.authMu.RUnlock()
	if !found {
		account, storeErr := s.Store.FindOrCreateAccount(context.Background(), username)
		if storeErr != nil {
			s.Logger.Error("login account lookup", "username", username, "error", storeErr)
			return rmc.ResponseError{Protocol: authenticationProtocol, Call: request.Call, Code: 0x80020001}
		}
		credentials = credential{
			PID: account.PID, Password: []byte("h7fyctiuucf"),
		}
		stored, storedFound, credentialErr := s.Store.AccountCredential(context.Background(), account.PID)
		if credentialErr != nil {
			s.Logger.Error("login credential lookup", "username", username, "error", credentialErr)
			return rmc.ResponseError{Protocol: authenticationProtocol, Call: request.Call, Code: 0x80020001}
		}
		if storedFound {
			credentials.Password = stored.Password
			credentials.PreHashed = stored.PreHashed
		}
		s.authMu.Lock()
		s.credentials[username] = credentials
		s.authMu.Unlock()
	}

	ciphertext, sessionKey, err := buildLoginCredential(
		credentials.PID, credentials.Password, credentials.PreHashed,
	)
	if err != nil {
		s.Logger.Error("build login credential", "username", username, "error", err)
		return rmc.ResponseError{Protocol: authenticationProtocol, Call: request.Call, Code: 0x80020001}
	}
	s.authMu.Lock()
	s.sessionKeys[credentials.PID] = append([]byte(nil), sessionKey...)
	s.authMu.Unlock()
	s.Identity.SetAuthenticatedPID(connection.Remote.IP.String(), credentials.PID)
	connection.UserPID = credentials.PID

	var out wire.Writer
	out.U32(0)
	out.U32(credentials.PID)
	out.QBuffer(ciphertext)
	out.QString(fmt.Sprintf(
		"prudp:/address=%s;port=%d;stream=3;sid=1;PID=2;CID=1;type=2",
		s.Config.PublicHost, s.Config.SecurePort,
	))
	protocols := []byte{0x0a, 0x0b, 0x0e, 0x15, 0x16, 0x19, 0x32, 0x3b, 0x3c, 0x3d}
	out.U32(uint32(len(protocols)))
	out.Bytes(protocols)
	out.QString(fmt.Sprintf(
		"prudp:/address=%s;port=%d;stream=3;sid=1;PID=2;CID=1;type=3",
		s.Config.PublicHost, s.Config.NATPort,
	))
	out.QString("")
	out.Bytes(loginTrailer)

	s.Logger.Info("login accepted", "remote", connection.Remote, "username", username, "pid", credentials.PID)
	return rmc.ResponseOK{
		Protocol: authenticationProtocol, Method: loginMethod,
		Call: request.Call, Return: out.Data(),
	}
}

// buildLoginCredential implements the password-based credential envelope used
// by Stranglehold PC. It is deliberately limited to login: the Ghostbusters
// PS3 KDF and per-service ticket/RequestTicket machinery are not present.
func buildLoginCredential(pid uint32, password []byte, preHashed bool) ([]byte, []byte, error) {
	key := deriveLoginKey(password, pid, preHashed)
	sessionKey := make([]byte, 16)
	if _, err := rand.Read(sessionKey); err != nil {
		return nil, nil, err
	}
	plain := binary.LittleEndian.AppendUint32(nil, pid)
	plain = append(plain, sessionKey...)
	ciphertext, err := encryptThenMAC(key, plain)
	if err != nil {
		return nil, nil, err
	}
	return ciphertext, sessionKey, nil
}

func deriveLoginKey(password []byte, pid uint32, preHashed bool) []byte {
	var digest [md5.Size]byte
	if preHashed {
		copy(digest[:], password)
		for range int(pid % 1024) {
			digest = md5.Sum(digest[:])
		}
		return digest[:]
	}
	digest = md5.Sum(password)
	for range 65000 + int(pid%1024) - 1 {
		digest = md5.Sum(digest[:])
	}
	return digest[:]
}

func encryptThenMAC(key, plain []byte) ([]byte, error) {
	cipher, err := rc4.NewCipher(key)
	if err != nil {
		return nil, err
	}
	ciphertext := make([]byte, len(plain))
	cipher.XORKeyStream(ciphertext, plain)
	mac := hmac.New(md5.New, key)
	_, _ = mac.Write(ciphertext)
	return append(ciphertext, mac.Sum(nil)...), nil
}

func decryptAndVerify(key, envelope []byte) ([]byte, error) {
	if len(envelope) < md5.Size {
		return nil, fmt.Errorf("credential envelope is too short")
	}
	ciphertext := envelope[:len(envelope)-md5.Size]
	tag := envelope[len(envelope)-md5.Size:]
	mac := hmac.New(md5.New, key)
	_, _ = mac.Write(ciphertext)
	if !hmac.Equal(tag, mac.Sum(nil)) {
		return nil, fmt.Errorf("credential MAC verification failed")
	}
	cipher, err := rc4.NewCipher(key)
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(ciphertext))
	cipher.XORKeyStream(plain, ciphertext)
	return plain, nil
}

func (s *Services) HandleConnectACK(connection *prudp.Connection, incoming []byte) ([]byte, error) {
	if len(incoming) < 5 || incoming[0] != 0 {
		return nil, fmt.Errorf("CONNECT credential payload is invalid")
	}
	outerSize := int(binary.LittleEndian.Uint32(incoming[1:5]))
	innerSizeOffset := 5 + outerSize
	if outerSize < 0 || innerSizeOffset+4 > len(incoming) {
		return nil, fmt.Errorf("CONNECT outer credential is truncated")
	}
	innerSize := int(binary.LittleEndian.Uint32(incoming[innerSizeOffset : innerSizeOffset+4]))
	innerStart := innerSizeOffset + 4
	if innerSize < md5.Size || innerStart+innerSize > len(incoming) {
		return nil, fmt.Errorf("CONNECT proof is truncated")
	}
	inner := incoming[innerStart : innerStart+innerSize]

	s.authMu.RLock()
	keys := make(map[uint32][]byte, len(s.sessionKeys))
	for pid, key := range s.sessionKeys {
		keys[pid] = append([]byte(nil), key...)
	}
	s.authMu.RUnlock()

	for pid, key := range keys {
		plain, err := decryptAndVerify(key, inner)
		if err != nil || len(plain) < 12 {
			continue
		}
		innerPID := binary.LittleEndian.Uint32(plain[:4])
		clientConnectionID := binary.LittleEndian.Uint32(plain[8:12])
		connection.UserPID = pid
		s.Identity.SetAuthenticatedPID(connection.Remote.IP.String(), pid)
		if innerPID != pid {
			s.Logger.Debug("CONNECT proof PID differs from session owner",
				"remote", connection.Remote, "inner_pid", innerPID, "pid", pid)
		}
		response := binary.LittleEndian.AppendUint32(nil, 4)
		response = binary.LittleEndian.AppendUint32(response, clientConnectionID+1)
		return response, nil
	}
	return nil, fmt.Errorf("CONNECT proof did not match an active login session")
}
